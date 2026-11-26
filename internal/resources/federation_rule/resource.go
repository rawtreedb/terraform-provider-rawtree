package federation_rule

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                     = &FederationRuleResource{}
	_ resource.ResourceWithConfigure        = &FederationRuleResource{}
	_ resource.ResourceWithImportState      = &FederationRuleResource{}
	_ resource.ResourceWithModifyPlan       = &FederationRuleResource{}
	_ resource.ResourceWithConfigValidators = &FederationRuleResource{}
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type FederationRuleResource struct {
	client *client.RawtreeClient
}

type FederationRuleModel struct {
	ID               types.String `tfsdk:"id"`
	Cluster          types.String `tfsdk:"cluster"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	ClusterID        types.String `tfsdk:"cluster_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	IssuerID         types.String `tfsdk:"issuer_id"`
	AllowedAudiences types.Set    `tfsdk:"allowed_audiences"`
	ClaimConditions  types.Map    `tfsdk:"claim_conditions"`
	PrincipalType    types.String `tfsdk:"principal_type"`
	PrincipalID      types.String `tfsdk:"principal_id"`
	Permission       types.String `tfsdk:"permission"`
	DatabaseRoles    types.Set    `tfsdk:"database_roles"`
	Database         types.String `tfsdk:"database"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

// ruleConfiguration is the create and replace body (RuleConfiguration).
// PUT replaces the whole configuration, so description is sent as null when unset.
type ruleConfiguration struct {
	Name             string            `json:"name"`
	Description      *string           `json:"description"`
	IssuerID         string            `json:"issuer_id"`
	AllowedAudiences []string          `json:"allowed_audiences"`
	ClaimConditions  map[string]string `json:"claim_conditions"`
	PrincipalType    string            `json:"principal_type"`
	PrincipalID      string            `json:"principal_id"`
	Permission       *string           `json:"permission,omitempty"`
	DatabaseRoles    []string          `json:"database_roles,omitempty"`
	Database         string            `json:"database"`
	Enabled          bool              `json:"enabled"`
}

// rule is the API representation (WorkloadFederationRule).
type rule struct {
	ruleConfiguration
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ClusterID      string `json:"cluster_id"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func NewResource() resource.Resource {
	return &FederationRuleResource{}
}

func (r *FederationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_rule"
}

func resourceSchema() schema.Schema {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a workload federation rule on a cluster. A rule lets tokens from a rawtree_federation_issuer that match its audiences and claim conditions act as a user or service account. Interactive use only: requires a user access token, which expires after 15 minutes and does not work in CI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Rule ID. Workloads pass it to the token exchange.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"cluster": schema.StringAttribute{
				Description:   "Cluster name or ID. Defaults to the provider cluster. Changing it replaces the rule.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"organization_id": schema.StringAttribute{
				Description:   "ID of the organization that owns the rule.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"cluster_id": schema.StringAttribute{
				Description:   "ID of the cluster that owns the rule.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"name": schema.StringAttribute{
				Description: "Name: lowercase letters and digits separated by single hyphens, 1-128 characters.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 128),
					stringvalidator.RegexMatches(namePattern, "must be lowercase letters and digits separated by single hyphens"),
				},
			},
			"description": schema.StringAttribute{
				Description: "Description, at most 2000 UTF-8 bytes.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtMost(2000)},
			},
			"issuer_id": schema.StringAttribute{
				Description: "ID of the rawtree_federation_issuer that signs the tokens.",
				Required:    true,
			},
			"allowed_audiences": schema.SetAttribute{
				Description: "Exact audiences. A token must match one of them.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"claim_conditions": schema.MapAttribute{
				Description: "Exact matches on top-level string claims, for example sub or repository_id. All must match; no wildcards. 1-32 entries; iss, aud, exp, nbf, iat and jti are not allowed.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Map{
					mapvalidator.SizeBetween(1, 32),
					mapvalidator.KeysAre(
						stringvalidator.LengthAtLeast(1),
						stringvalidator.NoneOf("iss", "aud", "exp", "nbf", "iat", "jti"),
					),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"principal_type": schema.StringAttribute{
				Description: "Identity the workload acts as: service_account or user.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.OneOf("service_account", "user")},
			},
			"principal_id": schema.StringAttribute{
				Description: "ID of the service account (rawtree_service_account.id) or user.",
				Required:    true,
			},
			"permission": schema.StringAttribute{
				Description: "Permission level, as for API keys: admin, read_write, write_only or read_only. Exactly one of permission or database_roles is required.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.OneOf("admin", "read_write", "write_only", "read_only")},
			},
			"database_roles": schema.SetAttribute{
				Description: "Database roles to grant. Exactly one of permission or database_roles is required.",
				Optional:    true,
				ElementType: types.StringType,
				Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"database": schema.StringAttribute{
				Description: "Default database for the workload. It does not restrict database-role grants.",
				Required:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the rule permits token exchange. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"created_at": schema.StringAttribute{
				Description:   "Creation time.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update time.",
				Computed:    true,
			},
		},
	}
}

func (r *FederationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *FederationRuleResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("permission"), path.MatchRoot("database_roles")),
	}
}

func (r *FederationRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan defaults cluster to the provider cluster on create.
func (r *FederationRuleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || !req.State.Raw.IsNull() || r.client == nil {
		return
	}
	var cluster types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("cluster"), &cluster)...)
	if cluster.IsUnknown() && r.client.Cluster != "" {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("cluster"), r.client.Cluster)...)
	}
}

func rulePath(id string) string {
	return "/v1/federation-rules/" + url.PathEscape(id)
}

func (r *FederationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cluster := plan.Cluster.ValueString()
	if cluster == "" {
		cluster = r.client.Cluster
	}
	body, diags := toConfiguration(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out rule
	if err := r.client.InCluster(ctx, cluster, http.MethodPost, "/v1/federation-rules", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Error creating federation rule", err.Error())
		return
	}
	state, diags := toModel(ctx, out, cluster)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *FederationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cluster := state.Cluster.ValueString()
	var out rule
	if err := r.client.InCluster(ctx, cluster, http.MethodGet, rulePath(state.ID.ValueString()), nil, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading federation rule", err.Error())
		return
	}
	newState, diags := toModel(ctx, out, cluster)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *FederationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state FederationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := toConfiguration(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	cluster := state.Cluster.ValueString()
	var out rule
	if err := r.client.InCluster(ctx, cluster, http.MethodPut, rulePath(state.ID.ValueString()), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Error updating federation rule", err.Error())
		return
	}
	newState, diags := toModel(ctx, out, cluster)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *FederationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FederationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.InCluster(ctx, state.Cluster.ValueString(), http.MethodDelete, rulePath(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting federation rule", err.Error())
	}
}

// ImportState accepts <cluster>/<id>.
func (r *FederationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	cluster, id, ok := strings.Cut(req.ID, "/")
	if !ok || cluster == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected <cluster>/<id>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster"), cluster)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func toConfiguration(ctx context.Context, m FederationRuleModel) (ruleConfiguration, diag.Diagnostics) {
	var diags diag.Diagnostics
	cfg := ruleConfiguration{
		Name:          m.Name.ValueString(),
		Description:   m.Description.ValueStringPointer(),
		IssuerID:      m.IssuerID.ValueString(),
		PrincipalType: m.PrincipalType.ValueString(),
		PrincipalID:   m.PrincipalID.ValueString(),
		Permission:    m.Permission.ValueStringPointer(),
		Database:      m.Database.ValueString(),
		Enabled:       m.Enabled.ValueBool(),
	}
	diags.Append(m.AllowedAudiences.ElementsAs(ctx, &cfg.AllowedAudiences, false)...)
	diags.Append(m.ClaimConditions.ElementsAs(ctx, &cfg.ClaimConditions, false)...)
	if !m.DatabaseRoles.IsNull() {
		diags.Append(m.DatabaseRoles.ElementsAs(ctx, &cfg.DatabaseRoles, false)...)
	}
	return cfg, diags
}

func toModel(ctx context.Context, out rule, cluster string) (FederationRuleModel, diag.Diagnostics) {
	var diags, d diag.Diagnostics
	m := FederationRuleModel{
		ID:             types.StringValue(out.ID),
		Cluster:        types.StringValue(cluster),
		OrganizationID: types.StringValue(out.OrganizationID),
		ClusterID:      types.StringValue(out.ClusterID),
		Name:           types.StringValue(out.Name),
		Description:    types.StringPointerValue(out.Description),
		IssuerID:       types.StringValue(out.IssuerID),
		PrincipalType:  types.StringValue(out.PrincipalType),
		PrincipalID:    types.StringValue(out.PrincipalID),
		Permission:     types.StringPointerValue(out.Permission),
		Database:       types.StringValue(out.Database),
		Enabled:        types.BoolValue(out.Enabled),
		CreatedAt:      types.StringValue(out.CreatedAt),
		UpdatedAt:      types.StringValue(out.UpdatedAt),
		DatabaseRoles:  types.SetNull(types.StringType),
	}
	m.AllowedAudiences, d = types.SetValueFrom(ctx, types.StringType, out.AllowedAudiences)
	diags.Append(d...)
	m.ClaimConditions, d = types.MapValueFrom(ctx, types.StringType, out.ClaimConditions)
	diags.Append(d...)
	// The API returns null or [] when permission is used instead.
	if len(out.DatabaseRoles) > 0 {
		m.DatabaseRoles, d = types.SetValueFrom(ctx, types.StringType, out.DatabaseRoles)
		diags.Append(d...)
	}
	return m, diags
}
