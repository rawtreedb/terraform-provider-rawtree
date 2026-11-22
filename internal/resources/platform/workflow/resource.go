package workflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.ResourceWithImportState = (*workflowResource)(nil)
	_ resource.ResourceWithIdentity    = (*workflowResource)(nil)
	_ resource.ResourceWithConfigure   = (*workflowResource)(nil)

	scopeName       = regexp.MustCompile(`^[^/[:space:]]+$`)
	namePattern     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	scopeValidators = []validator.String{stringvalidator.RegexMatches(scopeName, "use a name without slashes or whitespace")}
)

type workflowResource struct{ client *client.PlatformClient }

type model struct {
	Sinks        []sinkModel    `tfsdk:"sinks"`
	ID           types.String   `tfsdk:"id"`
	WorkflowID   types.String   `tfsdk:"workflow_id"`
	Organization types.String   `tfsdk:"organization"`
	Cluster      types.String   `tfsdk:"cluster"`
	Name         types.String   `tfsdk:"name"`
	Description  types.String   `tfsdk:"description"`
	Query        *queryModel    `tfsdk:"query"`
	Schedule     *scheduleModel `tfsdk:"schedule"`
}

type queryModel struct {
	Database types.String `tfsdk:"database"`
	SQL      types.String `tfsdk:"sql"`
}

type scheduleModel struct {
	Cron    types.String `tfsdk:"cron"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

func NewResource() resource.Resource { return &workflowResource{} }

func (r *workflowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow"
}

func (r *workflowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	clients, ok := req.ProviderData.(*client.Clients)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Clients, got %T.", req.ProviderData))
		return
	}
	r.client = clients.Platform
}

func (r *workflowResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{Attributes: map[string]identityschema.Attribute{
		"id": identityschema.StringAttribute{RequiredForImport: true, Description: "Workflow identity in organization/cluster/workflow-uuid format."},
	}}
}

func (r *workflowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a SQL workflow in a Tinybird RawTree cluster.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, Description: "Workflow identity in organization/cluster/workflow-uuid format."},
			"workflow_id": schema.StringAttribute{Computed: true, Description: "Workflow UUID assigned by the API."},
			"organization": schema.StringAttribute{
				Required: true, Description: "Organization name. Changing this replaces the workflow.",
				Validators: scopeValidators, PlanModifiers: replace,
			},
			"cluster": schema.StringAttribute{
				Required: true, Description: "Cluster name. Changing this replaces the workflow.",
				Validators: scopeValidators, PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Unique workflow name within the cluster. The API stores names in lowercase.",
				Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "use 1 to 64 letters, digits, underscores, or hyphens")},
			},
			"description": schema.StringAttribute{Optional: true, Description: "Optional description of up to 1,000 characters."},
			"query": schema.SingleNestedAttribute{
				Required: true, Description: "SQL query to run.",
				Attributes: map[string]schema.Attribute{
					"database": schema.StringAttribute{Required: true, Description: "Database in which to run the query."},
					"sql":      schema.StringAttribute{Required: true, Description: "A read query or an INSERT INTO ... SELECT statement."},
				},
			},
			"schedule": schema.SingleNestedAttribute{
				Optional: true, Description: "Automatic execution schedule. Omit for a manual-only workflow.",
				Attributes: map[string]schema.Attribute{
					"cron": schema.StringAttribute{Required: true, Description: "Five- or six-field UTC cron expression."},
					"enabled": schema.BoolAttribute{
						Optional: true, Computed: true, Default: booldefault.StaticBool(true),
						Description: "Whether automatic runs are enabled. Defaults to true.",
					},
				},
			},
			"sinks": sinksSchema(),
		},
	}
}

func (m model) scope() client.WorkflowScope {
	return client.WorkflowScope{Organization: m.Organization.ValueString(), Cluster: m.Cluster.ValueString()}
}

func (m model) definition() client.WorkflowDefinition {
	def := client.WorkflowDefinition{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueStringPointer(),
		Query:       client.WorkflowQuery{Database: m.Query.Database.ValueString(), SQL: m.Query.SQL.ValueString()},
	}
	if m.Schedule != nil {
		def.Schedule = &client.WorkflowSchedule{Cron: m.Schedule.Cron.ValueString(), Enabled: m.Schedule.Enabled.ValueBool()}
	}
	return def
}

func (m *model) setRemote(w client.Workflow, refresh bool) {
	m.readSinks(w, refresh)
	m.WorkflowID = types.StringValue(w.ID)
	m.ID = types.StringValue(strings.Join([]string{m.Organization.ValueString(), m.Cluster.ValueString(), w.ID}, "/"))
	// Preserve equivalent user input when the API normalizes names, descriptions or cron whitespace.
	if !strings.EqualFold(m.Name.ValueString(), w.Name) {
		m.Name = types.StringValue(w.Name)
	}
	remoteDescription := types.StringPointerValue(w.Description)
	if m.Description.IsNull() || m.Description.IsUnknown() || strings.TrimSpace(m.Description.ValueString()) != remoteDescription.ValueString() {
		m.Description = remoteDescription
	}
	m.Query = &queryModel{Database: types.StringValue(w.Query.Database), SQL: types.StringValue(w.Query.SQL)}
	if w.Schedule == nil {
		m.Schedule = nil
		return
	}
	cron := types.StringValue(w.Schedule.Cron)
	if m.Schedule != nil && strings.Join(strings.Fields(m.Schedule.Cron.ValueString()), " ") == w.Schedule.Cron {
		cron = m.Schedule.Cron
	}
	m.Schedule = &scheduleModel{Cron: cron, Enabled: types.BoolValue(w.Schedule.Enabled)}
}

func (m model) save(ctx context.Context, state *tfsdk.State, identity *tfsdk.ResourceIdentity) diag.Diagnostics {
	d := state.Set(ctx, &m)
	if identity != nil {
		d.Append(identity.SetAttribute(ctx, path.Root("id"), m.ID)...)
	}
	return d
}

func (r *workflowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	def := m.definition()
	def.Sinks = plannedSinks(m.Sinks, nil)
	w, err := r.client.CreateWorkflow(ctx, m.scope(), def)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create workflow", err.Error())
		return
	}
	m.setRemote(*w, false)
	resp.Diagnostics.Append(m.save(ctx, &resp.State, resp.Identity)...)
}

func (r *workflowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.client.GetWorkflow(ctx, m.scope(), m.WorkflowID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read workflow", err.Error())
		return
	}
	if w == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	m.setRemote(*w, true)
	resp.Diagnostics.Append(m.save(ctx, &resp.State, resp.Identity)...)
}

func (r *workflowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	var previous model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	def := m.definition()
	def.Sinks = plannedSinks(m.Sinks, previous.Sinks)
	w, err := r.client.UpdateWorkflow(ctx, m.scope(), previous.WorkflowID.ValueString(), def)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update workflow", err.Error())
		return
	}
	m.setRemote(*w, false)
	resp.Diagnostics.Append(m.save(ctx, &resp.State, resp.Identity)...)
}

func (r *workflowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteWorkflow(ctx, m.scope(), m.WorkflowID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot delete workflow", err.Error())
	}
}

func (r *workflowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := types.StringValue(req.ID)
	if req.ID == "" && req.Identity != nil {
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	parts := strings.Split(id.ValueString(), "/")
	if len(parts) != 3 || !scopeName.MatchString(parts[0]) || !scopeName.MatchString(parts[1]) || !client.ValidWorkflowID(parts[2]) {
		resp.Diagnostics.AddError("Invalid workflow import ID", "Use organization/cluster/workflow-uuid.")
		return
	}
	m := model{ID: id, Organization: types.StringValue(parts[0]), Cluster: types.StringValue(parts[1]), WorkflowID: types.StringValue(parts[2])}
	resp.Diagnostics.Append(m.save(ctx, &resp.State, resp.Identity)...)
}
