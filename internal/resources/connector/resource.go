package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &ConnectorResource{}
	_ resource.ResourceWithConfigure   = &ConnectorResource{}
	_ resource.ResourceWithImportState = &ConnectorResource{}
)

type ConnectorResource struct {
	client *client.RawtreeClient
}

type ConnectorModel struct {
	ID                    types.String       `tfsdk:"id"`
	Cluster               types.String       `tfsdk:"cluster"`
	Name                  types.String       `tfsdk:"name"`
	Type                  types.String       `tfsdk:"type"`
	Destinations          []DestinationModel `tfsdk:"destinations"`
	SettingsJSON          types.String       `tfsdk:"settings_json"`
	CredentialsJSON       types.String       `tfsdk:"credentials_json_wo"`
	CredentialsVersion    types.Int64        `tfsdk:"credentials_wo_version"`
	Status                types.String       `tfsdk:"status"`
	CredentialsConfigured types.Bool         `tfsdk:"credentials_configured"`
	SlotOwned             types.Bool         `tfsdk:"slot_owned"`
	Revision              types.Int64        `tfsdk:"revision"`
	CreatedAt             types.String       `tfsdk:"created_at"`
	UpdatedAt             types.String       `tfsdk:"updated_at"`
}

type DestinationModel struct {
	ID             types.String   `tfsdk:"id"`
	Database       types.String   `tfsdk:"database"`
	Table          types.String   `tfsdk:"table"`
	Topics         []types.String `tfsdk:"topics"`
	GroupID        types.String   `tfsdk:"group_id"`
	PostgresTables []types.String `tfsdk:"postgres_tables"`
	InitialLoad    types.Bool     `tfsdk:"initial_load"`
	NatsStream     types.String   `tfsdk:"nats_stream"`
	NatsConsumer   types.String   `tfsdk:"nats_consumer"`
}

// connectorItem is the API representation (ConnectorItem in the OpenAPI spec).
type connectorItem struct {
	ID                    string          `json:"id"`
	Name                  string          `json:"name"`
	Type                  string          `json:"type"`
	Destinations          []destination   `json:"destinations"`
	Settings              json.RawMessage `json:"settings"`
	Status                string          `json:"status"`
	CredentialsConfigured bool            `json:"credentials_configured"`
	SlotOwned             *bool           `json:"slot_owned"`
	Revision              int64           `json:"revision"`
	CreatedAt             string          `json:"created_at"`
	UpdatedAt             string          `json:"updated_at"`
}

// destination is a ConnectorDestination; source fields are inline and depend on the type.
type destination struct {
	ID             string   `json:"id,omitempty"`
	Database       string   `json:"database"`
	Table          string   `json:"table"`
	Topics         []string `json:"topics,omitempty"`
	GroupID        *string  `json:"group_id,omitempty"`
	PostgresTables []string `json:"postgres_tables,omitempty"`
	InitialLoad    *bool    `json:"initial_load,omitempty"`
	NatsStream     *string  `json:"nats_stream,omitempty"`
	NatsConsumer   *string  `json:"nats_consumer,omitempty"`
}

func NewResource() resource.Resource {
	return &ConnectorResource{}
}

func (r *ConnectorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connector"
}

func resourceSchema() schema.Schema {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	optionalString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Description: desc, Optional: true}
	}
	return schema.Schema{
		Description: "Manages a RawTree connector that streams from Kafka, PostgreSQL or NATS into RawTree tables. Interactive use only: requires a user access token, which expires after 15 minutes and does not work in CI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Connector ID.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"cluster": schema.StringAttribute{
				Description:   "Cluster name or ID. Defaults to the provider cluster.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Connector name.",
				Required:    true,
			},
			"type": schema.StringAttribute{
				Description:   "Source type: kafka, postgres or nats.",
				Required:      true,
				Validators:    []validator.String{stringvalidator.OneOf("kafka", "postgres", "nats")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"destinations": schema.ListNestedAttribute{
				Description: "Destination tables and the source each one reads. Set the source fields that match the connector type.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Destination ID.",
							Computed:    true,
						},
						"database": schema.StringAttribute{
							Description: "Destination database.",
							Required:    true,
						},
						"table": schema.StringAttribute{
							Description: "Destination table.",
							Required:    true,
						},
						"topics": schema.ListAttribute{
							Description: "Kafka topics to read.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"group_id": schema.StringAttribute{
							Description: "Kafka consumer group. Generated when omitted.",
							Optional:    true,
							Computed:    true,
						},
						"postgres_tables": schema.ListAttribute{
							Description: "PostgreSQL source tables as schema.table.",
							Optional:    true,
							ElementType: types.StringType,
						},
						"initial_load": schema.BoolAttribute{
							Description: "PostgreSQL only: copy the existing rows before streaming changes. Defaults to true, and only applies to destinations created with the connector.",
							Optional:    true,
							Computed:    true,
						},
						"nats_stream":   optionalString("NATS stream to read."),
						"nats_consumer": optionalString("Durable NATS consumer name."),
					},
				},
			},
			"settings_json": schema.StringAttribute{
				Description:   "Connector settings as a JSON object, without credentials. Only the keys set here are checked for drift.",
				Required:      true,
				PlanModifiers: []planmodifier.String{jsonSemanticEqual{}},
			},
			"credentials_json_wo": schema.StringAttribute{
				Description: "Write-only credentials as a JSON object, merged into settings_json when settings are sent. For example {\"password\":\"...\"} for postgres or {\"sasl\":{\"username\":\"...\",\"password\":\"...\"}} for kafka. Never stored in state. Requires Terraform 1.11 or later.",
				Optional:    true,
				Sensitive:   true,
				WriteOnly:   true,
			},
			"credentials_wo_version": schema.Int64Attribute{
				Description: "Change this value to send new credentials_json_wo to the API.",
				Optional:    true,
			},
			"status": schema.StringAttribute{
				Description:   "active or paused.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.OneOf("active", "paused")},
				PlanModifiers: keep,
			},
			"credentials_configured": schema.BoolAttribute{
				Description: "Whether the API holds credentials for the connector.",
				Computed:    true,
			},
			"slot_owned": schema.BoolAttribute{
				Description: "PostgreSQL only: whether the platform created the replication slot and drops it on delete.",
				Computed:    true,
			},
			"revision": schema.Int64Attribute{
				Description: "Connector revision.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description:   "Creation timestamp.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
		},
	}
}

func (r *ConnectorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *ConnectorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ConnectorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConnectorModel
	var credentials types.String
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("credentials_json_wo"), &credentials)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Cluster.IsUnknown() || plan.Cluster.IsNull() {
		plan.Cluster = types.StringValue(r.client.Cluster)
	}

	settings, err := mergedSettings(plan.SettingsJSON, credentials)
	if err != nil {
		resp.Diagnostics.AddError("Invalid connector settings", err.Error())
		return
	}
	body := map[string]any{
		"name":         plan.Name.ValueString(),
		"type":         plan.Type.ValueString(),
		"destinations": requestDestinations(plan.Destinations, nil, true),
		"settings":     settings,
	}
	wantStatus := plan.Status.ValueString()
	var item connectorItem
	if err := r.client.InCluster(ctx, plan.Cluster.ValueString(), http.MethodPost, "/v1/connectors", nil, body, &item); err != nil {
		resp.Diagnostics.AddError("Error creating connector", err.Error())
		return
	}
	resp.Diagnostics.Append(applyItem(&plan, &item)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create request has no status, so pausing at creation is a second call.
	if wantStatus == "paused" && item.Status != "paused" {
		if err := r.client.InCluster(ctx, plan.Cluster.ValueString(), http.MethodPatch, connectorPath(item.ID), nil, map[string]any{"status": "paused"}, &item); err != nil {
			resp.Diagnostics.AddError("Error pausing connector", err.Error())
			return
		}
		resp.Diagnostics.Append(applyItem(&plan, &item)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *ConnectorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConnectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var item connectorItem
	err := r.client.InCluster(ctx, state.Cluster.ValueString(), http.MethodGet, connectorPath(state.ID.ValueString()), nil, nil, &item)
	// A 404 for the cluster or the organization does not mean that the connector is gone.
	if client.IsAPIError(err, http.StatusNotFound, connectorNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading connector", err.Error())
		return
	}
	resp.Diagnostics.Append(applyItem(&state, &item)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConnectorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ConnectorModel
	var credentials types.String
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("credentials_json_wo"), &credentials)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{}
	if !plan.Name.Equal(state.Name) {
		body["name"] = plan.Name.ValueString()
	}
	planDest := requestDestinations(plan.Destinations, state.Destinations, false)
	if !reflect.DeepEqual(planDest, requestDestinations(state.Destinations, state.Destinations, false)) {
		body["destinations"] = planDest
	}
	if !plan.SettingsJSON.Equal(state.SettingsJSON) || !plan.CredentialsVersion.Equal(state.CredentialsVersion) {
		// Credentials are not in state, so they are resent from config with every settings change.
		settings, err := mergedSettings(plan.SettingsJSON, credentials)
		if err != nil {
			resp.Diagnostics.AddError("Invalid connector settings", err.Error())
			return
		}
		body["settings"] = settings
	}
	if !plan.Status.IsUnknown() && !plan.Status.Equal(state.Status) {
		body["status"] = plan.Status.ValueString()
	}

	var item connectorItem
	method := http.MethodPatch
	if len(body) == 0 {
		method, body = http.MethodGet, nil
	}
	if err := r.client.InCluster(ctx, state.Cluster.ValueString(), method, connectorPath(state.ID.ValueString()), nil, body, &item); err != nil {
		resp.Diagnostics.AddError("Error updating connector", err.Error())
		return
	}
	resp.Diagnostics.Append(applyItem(&plan, &item)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConnectorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConnectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.InCluster(ctx, state.Cluster.ValueString(), http.MethodDelete, connectorPath(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsAPIError(err, http.StatusNotFound, connectorNotFound) {
		resp.Diagnostics.AddError("Error deleting connector", err.Error())
	}
}

// ImportState accepts <cluster>/<connector_id>.
func (r *ConnectorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	cluster, id, ok := strings.Cut(req.ID, "/")
	if !ok || cluster == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected <cluster>/<connector_id>, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster"), cluster)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// connectorNotFound is the API message for a missing connector.
const connectorNotFound = "Connector not found."

func connectorPath(id string) string {
	return "/v1/connectors/" + url.PathEscape(id)
}

// requestDestinations builds the request destinations. On update, a destination
// keeps the ID of the saved destination with the same database and table, else
// of the saved destination at the same position. The API then updates it in
// place and keeps its consumer group, instead of creating a new one.
func requestDestinations(dests, saved []DestinationModel, creating bool) []destination {
	used := map[int]bool{}
	match := make([]int, len(dests))
	for j, d := range dests {
		match[j] = -1
		for i, s := range saved {
			if !used[i] && s.Database.Equal(d.Database) && s.Table.Equal(d.Table) {
				used[i], match[j] = true, i
				break
			}
		}
	}
	for j := range dests {
		if match[j] < 0 && j < len(saved) && !used[j] {
			used[j], match[j] = true, j
		}
	}
	out := make([]destination, 0, len(dests))
	for j, d := range dests {
		req := destination{
			Database:       d.Database.ValueString(),
			Table:          d.Table.ValueString(),
			Topics:         stringSlice(d.Topics),
			GroupID:        knownString(d.GroupID),
			PostgresTables: stringSlice(d.PostgresTables),
			NatsStream:     knownString(d.NatsStream),
			NatsConsumer:   knownString(d.NatsConsumer),
		}
		if creating {
			req.InitialLoad = knownBool(d.InitialLoad)
		}
		if match[j] >= 0 {
			s := saved[match[j]]
			req.ID = s.ID.ValueString()
			// A destination added to an existing connector cannot ask for an initial load.
			req.InitialLoad = knownBool(d.InitialLoad)
			if req.GroupID == nil {
				req.GroupID = knownString(s.GroupID)
			}
			if req.InitialLoad == nil {
				req.InitialLoad = knownBool(s.InitialLoad)
			}
		}
		out = append(out, req)
	}
	return out
}

// applyItem copies the API response into the model. settings_json keeps the
// configured string while every configured key matches the API value.
func applyItem(m *ConnectorModel, item *connectorItem) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(item.ID)
	// The API trims the name: keep the configured spelling when only that differs.
	if m.Name.IsNull() || m.Name.IsUnknown() || strings.TrimSpace(m.Name.ValueString()) != item.Name {
		m.Name = types.StringValue(item.Name)
	}
	m.Type = types.StringValue(item.Type)
	m.Status = types.StringValue(item.Status)
	m.CredentialsConfigured = types.BoolValue(item.CredentialsConfigured)
	m.SlotOwned = types.BoolPointerValue(item.SlotOwned)
	m.Revision = types.Int64Value(item.Revision)
	m.CreatedAt = types.StringValue(item.CreatedAt)
	m.UpdatedAt = types.StringValue(item.UpdatedAt)
	m.CredentialsJSON = types.StringNull()

	prior := m.Destinations
	m.Destinations = make([]DestinationModel, 0, len(item.Destinations))
	for i, d := range item.Destinations {
		// The API stores postgres tables as lowercase schema.table: keep the configured spelling.
		if i < len(prior) && samePostgresTables(prior[i].PostgresTables, d.PostgresTables) {
			d.PostgresTables = stringSlice(prior[i].PostgresTables)
		}
		m.Destinations = append(m.Destinations, DestinationModel{
			ID:             types.StringValue(d.ID),
			Database:       types.StringValue(d.Database),
			Table:          types.StringValue(d.Table),
			Topics:         stringValues(d.Topics),
			GroupID:        types.StringPointerValue(d.GroupID),
			PostgresTables: stringValues(d.PostgresTables),
			InitialLoad:    types.BoolPointerValue(d.InitialLoad),
			NatsStream:     types.StringPointerValue(d.NatsStream),
			NatsConsumer:   types.StringPointerValue(d.NatsConsumer),
		})
	}

	var remote any
	if len(item.Settings) > 0 {
		if err := json.Unmarshal(item.Settings, &remote); err != nil {
			diags.AddError("Invalid connector settings from API", err.Error())
			return diags
		}
	}
	if !m.SettingsJSON.IsNull() && !m.SettingsJSON.IsUnknown() {
		var local any
		if json.Unmarshal([]byte(m.SettingsJSON.ValueString()), &local) == nil && subset(local, remote) {
			return diags
		}
	}
	b, _ := json.Marshal(remote)
	m.SettingsJSON = types.StringValue(string(b))
	return diags
}

// neverReturned lists the settings that the API strips from its responses.
var neverReturned = map[string]bool{"username": true, "password": true, "client_key": true, "token": true, "seed": true}

// samePostgresTables reports whether the API list is the normalised form of the configured list.
func samePostgresTables(configured []types.String, api []string) bool {
	if len(configured) == 0 || len(configured) != len(api) {
		return false
	}
	for i, c := range configured {
		ref := strings.ToLower(c.ValueString())
		if !strings.Contains(ref, ".") {
			ref = "public." + ref
		}
		if ref != api[i] {
			return false
		}
	}
	return true
}

// subset reports whether every key in local has the same value in remote.
// Keys only the API returns (defaults) are ignored. The API trims and
// lowercases host, so host is compared in that form.
func subset(local, remote any) bool {
	switch l := local.(type) {
	case map[string]any:
		r, ok := remote.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range l {
			rv, ok := r[k]
			// The API never returns these credential fields.
			if neverReturned[k] && rv == nil {
				continue
			}
			// The API forces slot creation when a destination asks for an initial load.
			if k == "create_slot_if_missing" && v == false && rv == true {
				continue
			}
			if host, isString := v.(string); k == "host" && isString {
				v = strings.ToLower(strings.TrimSpace(host))
			}
			if !ok || !subset(v, rv) {
				return false
			}
		}
		return true
	case []any:
		r, ok := remote.([]any)
		if !ok || len(r) != len(l) {
			return false
		}
		for i := range l {
			if !subset(l[i], r[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(local, remote)
	}
}

// mergedSettings parses settings_json and deep-merges the write-only credentials into it.
func mergedSettings(settings, credentials types.String) (map[string]any, error) {
	var s map[string]any
	if err := json.Unmarshal([]byte(settings.ValueString()), &s); err != nil || s == nil {
		return nil, fmt.Errorf("settings_json must be a JSON object")
	}
	if credentials.IsNull() || credentials.IsUnknown() {
		return s, nil
	}
	var c map[string]any
	if err := json.Unmarshal([]byte(credentials.ValueString()), &c); err != nil || c == nil {
		// Do not echo the value, it holds secrets.
		return nil, fmt.Errorf("credentials_json_wo must be a JSON object")
	}
	merge(s, c)
	return s, nil
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				merge(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}

// jsonSemanticEqual keeps the state value when the planned JSON only differs in formatting or key order.
type jsonSemanticEqual struct{}

func (jsonSemanticEqual) Description(context.Context) string {
	return "Ignores JSON formatting and key order differences."
}

func (m jsonSemanticEqual) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (jsonSemanticEqual) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsUnknown() || req.PlanValue.IsNull() {
		return
	}
	var p, s any
	if json.Unmarshal([]byte(req.PlanValue.ValueString()), &p) != nil || json.Unmarshal([]byte(req.StateValue.ValueString()), &s) != nil {
		return
	}
	if reflect.DeepEqual(p, s) {
		resp.PlanValue = req.StateValue
	}
}

func knownString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func knownBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func stringSlice(vs []types.String) []string {
	if vs == nil {
		return nil
	}
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.ValueString()
	}
	return out
}

func stringValues(vs []string) []types.String {
	if vs == nil {
		return nil
	}
	out := make([]types.String, len(vs))
	for i, v := range vs {
		out[i] = types.StringValue(v)
	}
	return out
}
