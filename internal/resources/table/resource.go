package table

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &TableResource{}
	_ resource.ResourceWithConfigure   = &TableResource{}
	_ resource.ResourceWithImportState = &TableResource{}
	_ resource.ResourceWithModifyPlan  = &TableResource{}
)

type TableResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &TableResource{}
}

type TableModel struct {
	ID               types.String `tfsdk:"id"`
	Database         types.String `tfsdk:"database"`
	Name             types.String `tfsdk:"name"`
	SortingKey       types.String `tfsdk:"sorting_key"`
	TTL              types.String `tfsdk:"ttl"`
	SettingsJSON     types.String `tfsdk:"settings_json"`
	ReplacingEnable  types.Bool   `tfsdk:"replacing_enable"`
	StoredSortingKey types.String `tfsdk:"stored_sorting_key"`
	StoredTTL        types.String `tfsdk:"stored_ttl"`
}

// tableResponse is the shared part of the create, update and describe responses.
type tableResponse struct {
	Database   string  `json:"database"`
	SortingKey string  `json:"sorting_key"`
	TTL        *string `json:"ttl"`
}

type tableSettings struct {
	ReplacingEnable *bool `json:"replacing_enable"`
}

func (r *TableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table"
}

func (r *TableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func resourceSchema() schema.Schema {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a RawTree table. Columns are created from ingested data.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "`<database>/<table>`, or `<table>` when the database is not known.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"database": schema.StringAttribute{
				Description: "Database of the table. Defaults to the provider `database`, then to the API key's database.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					// Null in state is an import without a database: the first value must not drop the table.
					stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull()
					}, "Replaces the table when the database changes.", "Replaces the table when the database changes."),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description:   "Table name.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"sorting_key": schema.StringAttribute{
				Description:   "Comma-separated SQL sorting expressions. Empty lets the table pick a key per part. Existing parts keep the previous key until they are merged.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keep,
			},
			"ttl": schema.StringAttribute{
				Description: "Row-deletion TTL expression, optionally followed by `DELETE WHERE <condition>`. The API cannot remove a TTL: after it is set, you can change it but not unset it.",
				Optional:    true,
			},
			"settings_json": schema.StringAttribute{
				Description:   "Engine settings as a JSON object of booleans, numbers or strings. Key order and whitespace are ignored. The API masks setting values, so changes made outside Terraform are not detected, and removing a key does not reset the setting.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{settingsSemanticEqual{}},
			},
			"replacing_enable": schema.BoolAttribute{
				Description:   "Replace rows with the same sorting key on merge. Null when the table engine does not support it.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"stored_sorting_key": schema.StringAttribute{
				Description:   "Sorting key as the API stores it.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"stored_ttl": schema.StringAttribute{
				Description:   "TTL as the API stores it.",
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

func (r *TableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.RawtreeClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.RawtreeClient, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

// ModifyPlan marks the stored expressions unknown when their inputs change.
func (r *TableResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state TableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.SortingKey.Equal(state.SortingKey) {
		plan.StoredSortingKey = types.StringUnknown()
	}
	if !plan.TTL.Equal(state.TTL) {
		plan.StoredTTL = types.StringUnknown()
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *TableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	database := plan.Database.ValueString()
	if plan.Database.IsUnknown() || database == "" {
		database = r.client.Database
	}
	name := plan.Name.ValueString()

	body := map[string]any{"name": name}
	if known(plan.SortingKey) {
		body["sorting_key"] = plan.SortingKey.ValueString()
	}
	if known(plan.TTL) {
		body["ttl"] = plan.TTL.ValueString()
	}
	if known(plan.SettingsJSON) {
		settings, err := parseSettings(plan.SettingsJSON.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("settings_json"), "Invalid settings_json", err.Error())
			return
		}
		body["settings"] = settings
	}
	var out tableResponse
	if err := r.client.Data(ctx, http.MethodPost, "/v1/tables", dbQuery(database), body, &out); err != nil {
		resp.Diagnostics.AddError("Error creating table", err.Error())
		return
	}
	// The API reports the database it used, which also covers the key's default.
	if out.Database != "" {
		database = out.Database
	}
	plan.Database = optionalString(database)
	plan.ID = types.StringValue(tableID(database, name))
	applyMutation(&plan, &out)
	// Set the state now so a failed settings call does not orphan the table.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	settings, err := r.applyReplacing(ctx, database, name, plan.ReplacingEnable)
	if err != nil {
		resp.Diagnostics.AddError("Error updating table settings", err.Error())
		return
	}
	plan.ReplacingEnable = types.BoolPointerValue(settings.ReplacingEnable)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	database := state.Database.ValueString()
	name := state.Name.ValueString()

	var out struct {
		Table tableResponse `json:"table"`
	}
	err := r.client.Data(ctx, http.MethodGet, tablePath(name), dbQuery(database), nil, &out)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading table", err.Error())
		return
	}
	// Keep the configured expression while the stored form has not changed,
	// because the engine rewrites expressions (for example INTERVAL 1 DAY).
	if !equalPtr(state.StoredSortingKey, &out.Table.SortingKey) {
		state.SortingKey = types.StringValue(out.Table.SortingKey)
	}
	if !equalPtr(state.StoredTTL, out.Table.TTL) {
		state.TTL = types.StringPointerValue(out.Table.TTL)
	}
	state.StoredSortingKey = types.StringValue(out.Table.SortingKey)
	state.StoredTTL = types.StringPointerValue(out.Table.TTL)

	var settings tableSettings
	if err := r.client.Data(ctx, http.MethodGet, tablePath(name)+"/settings", dbQuery(database), nil, &settings); err != nil {
		resp.Diagnostics.AddError("Error reading table settings", err.Error())
		return
	}
	state.ReplacingEnable = types.BoolPointerValue(settings.ReplacingEnable)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state TableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	database := state.Database.ValueString()
	name := state.Name.ValueString()

	if plan.TTL.IsNull() && !state.TTL.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("ttl"), "A table TTL cannot be removed",
			"The RawTree API has no operation that removes a TTL. Keep `ttl` set, or replace the table (this deletes its data).")
		return
	}

	// The API accepts one configuration field per PATCH.
	var patches []map[string]any
	if known(plan.SortingKey) && !plan.SortingKey.Equal(state.SortingKey) {
		patches = append(patches, map[string]any{"sorting_key": plan.SortingKey.ValueString()})
	}
	if !plan.TTL.Equal(state.TTL) {
		patches = append(patches, map[string]any{"ttl": plan.TTL.ValueString()})
	}
	if !plan.SettingsJSON.Equal(state.SettingsJSON) && !plan.SettingsJSON.IsUnknown() {
		settings, err := parseSettings(plan.SettingsJSON.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("settings_json"), "Invalid settings_json", err.Error())
			return
		}
		if old, err := parseSettings(state.SettingsJSON.ValueString()); err == nil {
			for k := range old {
				if _, ok := settings[k]; !ok {
					resp.Diagnostics.AddAttributeWarning(path.Root("settings_json"), "Removed setting is not reset",
						fmt.Sprintf("Setting %q was removed from settings_json but the table keeps its current value. Reset it with ALTER TABLE ... RESET SETTING %s.", k, k))
				}
			}
		}
		if len(settings) > 0 {
			patches = append(patches, map[string]any{"settings": settings})
		}
	}

	// Save each applied field, so a later failure does not hide the earlier changes.
	done := state
	for _, body := range patches {
		var out tableResponse
		if err := r.client.Data(ctx, http.MethodPatch, tablePath(name), dbQuery(database), body, &out); err != nil {
			resp.Diagnostics.AddError("Error updating table", err.Error())
			return
		}
		switch {
		case body["sorting_key"] != nil:
			done.SortingKey = plan.SortingKey
		case body["ttl"] != nil:
			done.TTL = plan.TTL
		default:
			done.SettingsJSON = plan.SettingsJSON
		}
		done.StoredSortingKey = types.StringValue(out.SortingKey)
		done.StoredTTL = types.StringPointerValue(out.TTL)
		resp.Diagnostics.Append(resp.State.Set(ctx, &done)...)
	}
	plan.StoredSortingKey = done.StoredSortingKey
	plan.StoredTTL = done.StoredTTL

	replacing := plan.ReplacingEnable
	if replacing.Equal(state.ReplacingEnable) {
		replacing = types.BoolNull()
	}
	settings, err := r.applyReplacing(ctx, database, name, replacing)
	if err != nil {
		resp.Diagnostics.AddError("Error updating table settings", err.Error())
		return
	}
	plan.ReplacingEnable = types.BoolPointerValue(settings.ReplacingEnable)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Data(ctx, http.MethodDelete, tablePath(state.Name.ValueString()), dbQuery(state.Database.ValueString()), nil, nil)
	// The API returns 400, not 404, when the table does not exist.
	if err != nil && !client.IsNotFound(err) && !client.IsAPIError(err, http.StatusBadRequest, "Table not found.") {
		resp.Diagnostics.AddError("Error deleting table", err.Error())
	}
}

// ImportState accepts `<database>/<table>` or `<table>` (provider database).
func (r *TableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	database, name, ok := strings.Cut(req.ID, "/")
	if !ok {
		name = req.ID
		database = ""
		if r.client != nil {
			database = r.client.Database
		}
	}
	if name == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected `<database>/<table>` or `<table>`.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), tableID(database, name))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), optionalString(database))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// applyReplacing sets replacing_enable when it is known, then returns the
// effective table settings.
func (r *TableResource) applyReplacing(ctx context.Context, database, name string, value types.Bool) (*tableSettings, error) {
	if known(value) {
		body := map[string]bool{"replacing_enable": value.ValueBool()}
		if err := r.client.Data(ctx, http.MethodPatch, tablePath(name)+"/settings", dbQuery(database), body, nil); err != nil {
			return nil, err
		}
	}
	var settings tableSettings
	if err := r.client.Data(ctx, http.MethodGet, tablePath(name)+"/settings", dbQuery(database), nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// applyMutation stores the API form of the expressions. Configured values are
// kept so the state matches the plan; unknown ones take the API value.
func applyMutation(m *TableModel, out *tableResponse) {
	if m.SortingKey.IsUnknown() {
		m.SortingKey = types.StringValue(out.SortingKey)
	}
	m.StoredSortingKey = types.StringValue(out.SortingKey)
	m.StoredTTL = types.StringPointerValue(out.TTL)
}

// parseSettings decodes settings_json and checks the value types the API accepts.
func parseSettings(s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(s), &settings); err != nil {
		return nil, fmt.Errorf("settings_json must be a JSON object: %w", err)
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch settings[k].(type) {
		case bool, float64, string:
		default:
			return nil, fmt.Errorf("setting %q must be a boolean, number or string", k)
		}
	}
	return settings, nil
}

// settingsSemanticEqual keeps the state value when the planned JSON decodes to
// the same settings, so key order and whitespace do not cause a diff.
type settingsSemanticEqual struct{}

func (settingsSemanticEqual) Description(context.Context) string {
	return "Ignores key order and whitespace in settings_json."
}

func (m settingsSemanticEqual) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (settingsSemanticEqual) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !known(req.PlanValue) || !known(req.StateValue) {
		return
	}
	var planned, stored any
	if json.Unmarshal([]byte(req.PlanValue.ValueString()), &planned) != nil || json.Unmarshal([]byte(req.StateValue.ValueString()), &stored) != nil {
		return
	}
	if reflect.DeepEqual(planned, stored) {
		resp.PlanValue = req.StateValue
	}
}

func tablePath(name string) string {
	return "/v1/tables/" + url.PathEscape(name)
}

// dbQuery omits the database when it is empty, so the API key's database applies.
func dbQuery(database string) url.Values {
	if database == "" {
		return nil
	}
	return url.Values{"database": {database}}
}

func tableID(database, name string) string {
	if database == "" {
		return name
	}
	return database + "/" + name
}

func known(v interface {
	IsNull() bool
	IsUnknown() bool
}) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func optionalString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// equalPtr compares a stored state value with an API value; both may be null.
func equalPtr(stored types.String, api *string) bool {
	if stored.IsNull() || stored.IsUnknown() {
		return api == nil
	}
	return api != nil && stored.ValueString() == *api
}
