package workflow

import (
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

type sinkModel struct {
	ID    types.String `tfsdk:"id"`
	HTTP  *httpSink    `tfsdk:"http"`
	Table *tableSink   `tfsdk:"table"`
	Slack *slackSink   `tfsdk:"slack"`
}

type httpSink struct {
	URL     types.String            `tfsdk:"url"`
	Headers map[string]types.String `tfsdk:"headers"`
}

type tableSink struct {
	Database types.String `tfsdk:"database"`
	Table    types.String `tfsdk:"table"`
}

type slackSink struct {
	IntegrationID types.String `tfsdk:"integration_id"`
	ChannelID     types.String `tfsdk:"channel_id"`
}

func sinksSchema() schema.ListNestedAttribute {
	sibling := path.MatchRelative().AtParent()
	return schema.ListNestedAttribute{
		Optional:    true,
		Description: "Ordered list of up to five result destinations. Set exactly one of http, table, and slack per entry.",
		Validators:  []validator.List{listvalidator.SizeAtMost(5)},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Sink UUID assigned by the API."},
			"http": schema.SingleNestedAttribute{
				Optional: true, Description: "Send result rows to an HTTP endpoint.",
				Validators: []validator.Object{objectvalidator.ExactlyOneOf(sibling.AtName("table"), sibling.AtName("slack"))},
				Attributes: map[string]schema.Attribute{
					"url": schema.StringAttribute{
						Required: true, Sensitive: true, Description: "Webhook URL. Treat URLs containing credentials as secrets.",
					},
					"headers": schema.MapAttribute{
						Optional: true, ElementType: types.StringType, Sensitive: true,
						Description: "HTTP headers. The API never returns values. Omit to preserve existing headers on the same destination, or use an empty map to clear them.",
					},
				},
			},
			"table": schema.SingleNestedAttribute{
				Optional: true, Description: "Write result rows to a table.",
				Attributes: map[string]schema.Attribute{
					"database": schema.StringAttribute{Required: true, Description: "Destination database name."},
					"table":    schema.StringAttribute{Required: true, Description: "Destination table name."},
				},
			},
			"slack": schema.SingleNestedAttribute{
				Optional: true, Description: "Post results through a connected Slack integration.",
				Attributes: map[string]schema.Attribute{
					"integration_id": schema.StringAttribute{Required: true, Description: "UUID of the connected Slack integration."},
					"channel_id":     schema.StringAttribute{Required: true, Description: "Stable Slack channel ID."},
				},
			},
		}},
	}
}

// The server normalizes HTTP URLs, including adding a slash to a bare origin.
func canonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	switch u.Scheme {
	case "https":
		u.Host = strings.TrimSuffix(u.Host, ":443")
	case "http":
		u.Host = strings.TrimSuffix(u.Host, ":80")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// A destination owns its ID and any unreadable HTTP credentials. Match by target,
// never by list position, so reordering or changing a URL cannot move credentials.
type sinkTarget struct{ kind, first, second string }

func (s sinkModel) target() sinkTarget {
	switch {
	case s.HTTP != nil:
		return sinkTarget{"http", canonicalURL(s.HTTP.URL.ValueString()), ""}
	case s.Table != nil:
		return sinkTarget{"table", s.Table.Database.ValueString(), s.Table.Table.ValueString()}
	case s.Slack != nil:
		return sinkTarget{"slack", s.Slack.IntegrationID.ValueString(), s.Slack.ChannelID.ValueString()}
	default:
		return sinkTarget{}
	}
}

func (s sinkModel) input(previous sinkModel) client.WorkflowSink {
	sink := client.WorkflowSink{ID: previous.ID.ValueString(), Type: s.target().kind}
	switch {
	case s.HTTP != nil:
		sink.Settings.URL = s.HTTP.URL.ValueStringPointer()
		if s.HTTP.Headers != nil {
			headers := make(map[string]*string, len(s.HTTP.Headers))
			for k, v := range s.HTTP.Headers {
				headers[k] = v.ValueStringPointer()
			}
			sink.Settings.Headers = &headers
		}
	case s.Table != nil:
		sink.Settings.Database = s.Table.Database.ValueString()
		sink.Settings.Table = s.Table.Table.ValueString()
	case s.Slack != nil:
		sink.Settings.IntegrationID = s.Slack.IntegrationID.ValueString()
		sink.Settings.ChannelID = s.Slack.ChannelID.ValueString()
	}
	return sink
}

func plannedSinks(plan, prior []sinkModel) *[]client.WorkflowSink {
	remaining := slices.Clone(prior)
	changed := len(plan) != len(prior)
	inputs := make([]client.WorkflowSink, 0, len(plan))
	for i, s := range plan {
		target := s.target()
		old := takeSink(&remaining, s, false)
		if i >= len(prior) || prior[i].target() != target || !prior[i].ID.Equal(old.ID) {
			changed = true
		}
		if s.HTTP != nil && s.HTTP.Headers != nil && (old.HTTP == nil || old.HTTP.Headers == nil || !maps.Equal(s.HTTP.Headers, old.HTTP.Headers)) {
			changed = true
		}
		inputs = append(inputs, s.input(old))
	}
	if !changed {
		return nil
	}
	return &inputs
}

func (m *model) readSinks(w client.Workflow, refresh bool) {
	if w.Sinks == nil || len(*w.Sinks) == 0 {
		if m.Sinks != nil {
			m.Sinks = []sinkModel{}
		}
		return
	}
	remaining := slices.Clone(m.Sinks)
	result := make([]sinkModel, 0, len(*w.Sinks))
	for i, s := range *w.Sinks {
		v := s.Settings
		sink := sinkModel{ID: types.StringValue(s.ID)}
		switch s.Type {
		case "http":
			sink.HTTP = &httpSink{URL: types.StringPointerValue(v.URL)}
			var old sinkModel
			if refresh {
				old = takeSink(&remaining, sink, true)
			} else if i < len(m.Sinks) && m.Sinks[i].target() == sink.target() {
				// Create and PATCH return the submitted sinks in order.
				old = m.Sinks[i]
			}
			if old.HTTP != nil {
				sink.HTTP.URL = old.HTTP.URL
				sink.HTTP.Headers = readHeaders(old.HTTP.Headers, v.HeaderNames)
			}
		case "table":
			sink.Table = &tableSink{Database: types.StringValue(v.Database), Table: types.StringValue(v.Table)}
		case "slack":
			sink.Slack = &slackSink{IntegrationID: types.StringValue(v.IntegrationID), ChannelID: types.StringValue(v.ChannelID)}
		}
		result = append(result, sink)
	}
	m.Sinks = result
}

// Multiple HTTP sinks can use the same URL. Consume a match only once. During
// refresh, only the same server-side sink can inherit values from prior state.
func takeSink(remaining *[]sinkModel, desired sinkModel, refresh bool) sinkModel {
	matches := *remaining
	first := -1
	for i, previous := range matches {
		if previous.target() != desired.target() || (refresh && !previous.ID.Equal(desired.ID)) {
			continue
		}
		if first < 0 {
			first = i
		}
		if desired.HTTP == nil || maps.Equal(desired.HTTP.Headers, previous.HTTP.Headers) {
			first = i
			break
		}
	}
	if first < 0 {
		return sinkModel{}
	}
	match := matches[first]
	*remaining = slices.Delete(matches, first, first+1)
	return match
}

func readHeaders(prior map[string]types.String, names []string) map[string]types.String {
	if prior == nil {
		return nil
	}
	result := make(map[string]types.String, len(names))
	for _, name := range names {
		key, value := name, types.StringNull()
		for oldKey, oldValue := range prior {
			if strings.EqualFold(oldKey, name) {
				key, value = oldKey, oldValue
				break
			}
		}
		result[key] = value
	}
	return result
}
