package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &WorkflowResource{}
	_ resource.ResourceWithConfigure   = &WorkflowResource{}
	_ resource.ResourceWithImportState = &WorkflowResource{}
)

type WorkflowResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &WorkflowResource{}
}

func (r *WorkflowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow"
}

func (r *WorkflowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *WorkflowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.RawtreeClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.RawtreeClient, got: %T", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *WorkflowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Database.IsUnknown() || plan.Database.IsNull() {
		if r.client.Database == "" {
			resp.Diagnostics.AddAttributeError(path.Root("database"), "Missing database",
				"Set the workflow 'database' attribute or the provider 'database' attribute.")
			return
		}
		plan.Database = types.StringValue(r.client.Database)
	}
	body, diags := requestBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w apiWorkflow
	if err := r.client.Data(ctx, http.MethodPost, "/v1/workflows", nil, body, &w); err != nil {
		created, ok := r.createdDespite(ctx, err, plan.Name.ValueString())
		if !ok {
			resp.Diagnostics.AddError("Failed to create workflow", err.Error())
			return
		}
		w = created
		resp.Diagnostics.AddWarning("Workflow created, schedule not confirmed",
			"The API created the workflow but did not confirm its schedule: "+err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(w, plan, nil, true))...)
	resp.Diagnostics.Append(saveSinkURLs(ctx, resp.Private, w)...)
}

// createdDespite finds the workflow that a failed create call left behind.
// The API returns these two errors after it stored the definition, so without
// this lookup the workflow is an orphan and the retry fails on its name.
func (r *WorkflowResource) createdDespite(ctx context.Context, err error, name string) (apiWorkflow, bool) {
	if !client.IsAPIError(err, http.StatusServiceUnavailable, "Could not confirm workflow creation.") &&
		!client.IsAPIError(err, http.StatusConflict, "The workflow schedule was created concurrently.") {
		return apiWorkflow{}, false
	}
	var list struct {
		Workflows []apiWorkflow `json:"workflows"`
	}
	if err := r.client.Data(ctx, http.MethodGet, "/v1/workflows", nil, nil, &list); err != nil {
		return apiWorkflow{}, false
	}
	// Names are unique in a cluster, and the API stores them trimmed and in lower case.
	for _, w := range list.Workflows {
		if w.Name == strings.ToLower(strings.TrimSpace(name)) {
			return w, true
		}
	}
	return apiWorkflow{}, false
}

func (r *WorkflowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w apiWorkflow
	if err := r.client.Data(ctx, http.MethodGet, workflowPath(state.ID.ValueString()), nil, nil, &w); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read workflow", err.Error())
		return
	}
	stored, _ := req.Private.GetKey(ctx, sinkURLsKey)
	urls := map[string]string{}
	_ = json.Unmarshal(stored, &urls)
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(w, state, urls, false))...)
	resp.Diagnostics.Append(saveSinkURLs(ctx, resp.Private, w)...)
}

func (r *WorkflowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := requestBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w apiWorkflow
	if err := r.client.Data(ctx, http.MethodPatch, workflowPath(plan.ID.ValueString()), nil, body, &w); err != nil {
		resp.Diagnostics.AddError("Failed to update workflow", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(w, plan, nil, true))...)
	resp.Diagnostics.Append(saveSinkURLs(ctx, resp.Private, w)...)
}

func (r *WorkflowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Data(ctx, http.MethodDelete, workflowPath(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete workflow", err.Error())
	}
}

func (r *WorkflowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func workflowPath(id string) string {
	return "/v1/workflows/" + url.PathEscape(id)
}

// requestBody builds a WorkflowDefinition, also valid as a full UpdateWorkflow:
// null description and schedule clear them, and sinks replace the whole list.
func requestBody(ctx context.Context, m workflowModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := map[string]any{
		"name":        m.Name.ValueString(),
		"description": m.Description.ValueStringPointer(),
		"query":       map[string]any{"database": m.Database.ValueString(), "sql": m.SQL.ValueString()},
		"schedule":    nil,
	}
	if m.Schedule != nil {
		body["schedule"] = map[string]any{"cron": m.Schedule.Cron.ValueString(), "enabled": m.Schedule.Enabled.ValueBool()}
	}
	sinks := []map[string]any{}
	for _, s := range m.Sinks {
		item := map[string]any{"type": s.kind()}
		// Existing sink IDs make the API update the sink instead of recreating it.
		if !s.ID.IsUnknown() && !s.ID.IsNull() {
			item["id"] = s.ID.ValueString()
		}
		switch {
		case s.Table != nil:
			item["settings"] = map[string]any{"database": s.Table.Database.ValueString(), "table": s.Table.Table.ValueString()}
		case s.HTTP != nil:
			// Always send the full header map: omitting it would keep stale headers.
			headers := map[string]string{}
			if !s.HTTP.Headers.IsNull() {
				diags.Append(s.HTTP.Headers.ElementsAs(ctx, &headers, false)...)
			}
			item["settings"] = map[string]any{"url": s.HTTP.URL.ValueString(), "headers": headers}
		case s.Slack != nil:
			settings := map[string]any{"integration_id": s.Slack.IntegrationID.ValueString()}
			// Send only the configured channel field; the other one is computed.
			if known(s.Slack.ChannelID) {
				settings["channel_id"] = s.Slack.ChannelID.ValueString()
			} else {
				settings["channel_name"] = s.Slack.ChannelName.ValueString()
			}
			item["settings"] = settings
		}
		sinks = append(sinks, item)
	}
	body["sinks"] = sinks
	return body, diags
}

// sinkURLsKey is the private state key for the sink URLs as the API stores them.
const sinkURLsKey = "sink_urls"

// privateState is the part of the framework private state that this resource uses.
type privateState interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// saveSinkURLs records the API form of each HTTP sink URL. Read compares
// against it, so a URL that the API rewrites does not show as drift.
func saveSinkURLs(ctx context.Context, private privateState, w apiWorkflow) diag.Diagnostics {
	// The framework sets the private state; unit tests do not.
	if v := reflect.ValueOf(private); !v.IsValid() || v.IsNil() {
		return nil
	}
	urls := map[string]string{}
	for _, s := range w.Sinks {
		if s.Type == "http" && s.Settings.URL != nil {
			urls[s.ID] = *s.Settings.URL
		}
	}
	b, _ := json.Marshal(urls)
	return private.SetKey(ctx, sinkURLsKey, b)
}

// toModel maps a WorkflowItem to state. prior (plan or state) supplies the
// sink order and the values the API does not return, such as header values.
// applied is true after a create or update: the API then holds what prior
// sent. On a read, urls holds the API form of each sink URL at the last apply.
func toModel(w apiWorkflow, prior workflowModel, urls map[string]string, applied bool) workflowModel {
	m := workflowModel{
		ID:          types.StringValue(w.ID),
		Name:        keep(prior.Name, w.Name, func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }),
		Description: types.StringPointerValue(w.Description),
		Database:    types.StringValue(w.Query.Database),
		SQL:         types.StringValue(w.Query.SQL),
	}
	// The API trims the description and stores a blank one as null.
	if api := ""; known(prior.Description) {
		if w.Description != nil {
			api = *w.Description
		}
		if strings.TrimSpace(prior.Description.ValueString()) == api {
			m.Description = prior.Description
		}
	}
	if w.Schedule != nil {
		cron := types.StringValue(w.Schedule.Cron)
		if prior.Schedule != nil {
			// The API collapses whitespace in the expression.
			cron = keep(prior.Schedule.Cron, w.Schedule.Cron, func(s string) string { return strings.Join(strings.Fields(s), " ") })
		}
		m.Schedule = &scheduleModel{Cron: cron, Enabled: types.BoolValue(w.Schedule.Enabled)}
	}
	if prior.Sinks != nil {
		m.Sinks = []sinkModel{}
	}
	used := make([]bool, len(w.Sinks))
	match := make([]int, len(prior.Sinks))
	for j, p := range prior.Sinks {
		if match[j] = matchSink(w.Sinks, used, p); match[j] >= 0 {
			used[match[j]] = true
		}
	}
	// The API returns sinks in request order: a sink that it rewrote matches by position.
	for j, p := range prior.Sinks {
		if match[j] < 0 && j < len(w.Sinks) && !used[j] && w.Sinks[j].Type == p.kind() {
			used[j], match[j] = true, j
		}
	}
	for j, p := range prior.Sinks {
		if match[j] >= 0 {
			m.Sinks = append(m.Sinks, toSink(w.Sinks[match[j]], &p, urls, applied))
		}
	}
	for i, a := range w.Sinks {
		if !used[i] {
			m.Sinks = append(m.Sinks, toSink(a, nil, urls, applied))
		}
	}
	return m
}

// matchSink finds the API sink for a prior sink, by ID or else by target.
func matchSink(sinks []apiSink, used []bool, p sinkModel) int {
	if known(p.ID) {
		for i, a := range sinks {
			if !used[i] && a.ID == p.ID.ValueString() {
				return i
			}
		}
	}
	for i, a := range sinks {
		if !used[i] && sameTarget(a, p) {
			return i
		}
	}
	return -1
}

func sameTarget(a apiSink, p sinkModel) bool {
	if a.Type != p.kind() {
		return false
	}
	s := a.Settings
	switch a.Type {
	case "table":
		return s.Database == p.Table.Database.ValueString() && s.Table == p.Table.Table.ValueString()
	case "http":
		return s.URL != nil && *s.URL == normalizeURL(p.HTTP.URL.ValueString())
	case "slack":
		if s.IntegrationID != strings.ToLower(p.Slack.IntegrationID.ValueString()) {
			return false
		}
		if known(p.Slack.ChannelID) {
			return s.ChannelID == p.Slack.ChannelID.ValueString()
		}
		return sameChannelName(s.ChannelName, p.Slack.ChannelName.ValueString())
	}
	return false
}

func toSink(a apiSink, p *sinkModel, urls map[string]string, applied bool) sinkModel {
	s := sinkModel{ID: types.StringValue(a.ID)}
	if p != nil && p.kind() != a.Type {
		p = nil
	}
	switch a.Type {
	case "table":
		s.Table = &tableSinkModel{Database: types.StringValue(a.Settings.Database), Table: types.StringValue(a.Settings.Table)}
	case "http":
		h := &httpSinkModel{URL: types.StringPointerValue(a.Settings.URL), Headers: types.MapNull(types.StringType)}
		if p != nil {
			// The URL is null when the API cannot decrypt it.
			if a.Settings.URL == nil || known(p.HTTP.URL) && sameURL(p.HTTP.URL.ValueString(), *a.Settings.URL, urls[a.ID], applied) {
				h.URL = p.HTTP.URL
			}
			h.Headers = p.HTTP.Headers
		}
		h.Headers = mergeHeaders(a.Settings.HeaderNames, h.Headers)
		s.HTTP = h
	case "slack":
		sl := &slackSinkModel{
			IntegrationID: types.StringValue(a.Settings.IntegrationID),
			ChannelID:     types.StringValue(a.Settings.ChannelID),
			ChannelName:   types.StringValue(a.Settings.ChannelName),
		}
		// Keep the configured spelling, for example "#alerts" for "alerts".
		if p != nil && known(p.Slack.ChannelName) && sameChannelName(a.Settings.ChannelName, p.Slack.ChannelName.ValueString()) {
			sl.ChannelName = p.Slack.ChannelName
		}
		if p != nil {
			sl.IntegrationID = keep(p.Slack.IntegrationID, a.Settings.IntegrationID, strings.ToLower)
		}
		s.Slack = sl
	}
	return s
}

// keep returns the prior value when the API value is its normalised form, so
// the state matches the configuration. Otherwise it returns the API value.
func keep(prior types.String, api string, normalize func(string) string) types.String {
	if known(prior) && normalize(prior.ValueString()) == api {
		return prior
	}
	return types.StringValue(api)
}

// sameURL reports whether the API URL is the stored form of the configured URL.
func sameURL(configured, api, lastApplied string, applied bool) bool {
	// After an apply, the API holds what the provider sent. On a read, an API
	// value that did not change since the last apply is not drift.
	if applied || lastApplied != "" && lastApplied == api {
		return true
	}
	return normalizeURL(configured) == api
}

// normalizeURL mirrors how the API stores an HTTP sink URL: lowercase scheme
// and host, no default port, and "/" for an empty path. It matches sinks
// before the first apply and after an import, when no stored form is recorded.
func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if p := u.Port(); p == "80" && u.Scheme == "http" || p == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// mergeHeaders keeps the prior header values while the API reports the same
// header names. Otherwise it stores the reported names with empty values, so
// the next plan sends the configured values again.
func mergeHeaders(names []string, prior types.Map) types.Map {
	if !prior.IsNull() && !prior.IsUnknown() {
		priorNames := map[string]bool{}
		for k := range prior.Elements() {
			priorNames[strings.ToLower(k)] = true
		}
		same := len(priorNames) == len(names)
		for _, n := range names {
			same = same && priorNames[strings.ToLower(n)]
		}
		if same {
			return prior
		}
	}
	if len(names) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.MapNull(types.StringType)
	}
	elems := map[string]attr.Value{}
	for _, n := range names {
		elems[n] = types.StringValue("")
	}
	return types.MapValueMust(types.StringType, elems)
}

func sameChannelName(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(a, "#"), strings.TrimPrefix(b, "#"))
}

func known(v types.String) bool {
	return !v.IsNull() && !v.IsUnknown()
}
