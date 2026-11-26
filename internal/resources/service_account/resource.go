package service_account

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &ServiceAccountResource{}
	_ resource.ResourceWithConfigure   = &ServiceAccountResource{}
	_ resource.ResourceWithImportState = &ServiceAccountResource{}
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type ServiceAccountResource struct {
	client *client.RawtreeClient
}

type ServiceAccountModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Role           types.String `tfsdk:"role"`
}

// serviceAccount is the API representation (ServiceAccount in the OpenAPI spec).
type serviceAccount struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	Role           string  `json:"role"`
}

type createRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Role        string  `json:"role"`
}

func NewResource() resource.Resource {
	return &ServiceAccountResource{}
}

func (r *ServiceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func resourceSchema() schema.Schema {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a RawTree service account in the provider organization. The API has no update endpoint, so any change replaces the service account. Interactive use only: requires a user access token, which expires after 15 minutes and does not work in CI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Service account ID. Use it as principal_id in a rawtree_federation_rule.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"organization_id": schema.StringAttribute{
				Description:   "ID of the organization that owns the service account.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"name": schema.StringAttribute{
				Description:   "Name, unique within the organization: lowercase letters and digits separated by single hyphens, 1-128 characters.",
				Required:      true,
				PlanModifiers: replace,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 128),
					stringvalidator.RegexMatches(namePattern, "must be lowercase letters and digits separated by single hyphens"),
				},
			},
			"description": schema.StringAttribute{
				Description:   "Description, at most 2000 UTF-8 bytes.",
				Optional:      true,
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.LengthAtMost(2000)},
			},
			"role": schema.StringAttribute{
				Description:   "Stored organization role: member (default) or admin. API key and federation permissions are configured separately.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("member"),
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf("member", "admin")},
			},
		},
	}
}

func (r *ServiceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *ServiceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// orgID prefers the organization stored in state, so a changed provider
// organization does not point Read or Delete at another organization.
func (r *ServiceAccountResource) orgID(ctx context.Context, m ServiceAccountModel) (string, error) {
	if v := m.OrganizationID.ValueString(); v != "" {
		return v, nil
	}
	return r.client.OrganizationID(ctx)
}

func collectionPath(orgID string) string {
	return fmt.Sprintf("/v1/organizations/%s/service-accounts", url.PathEscape(orgID))
}

func (r *ServiceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServiceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	orgID, err := r.client.OrganizationID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	body := createRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Role:        plan.Role.ValueString(),
	}
	var sa serviceAccount
	if err := r.client.Global(ctx, http.MethodPost, collectionPath(orgID), nil, body, &sa); err != nil {
		resp.Diagnostics.AddError("Error creating service account", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(sa))...)
}

func (r *ServiceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServiceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	orgID, err := r.orgID(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	// The API has no GET by ID: list and find.
	var list []serviceAccount
	if err := r.client.Global(ctx, http.MethodGet, collectionPath(orgID), nil, nil, &list); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading service accounts", err.Error())
		return
	}
	for _, sa := range list {
		if sa.ID == state.ID.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, toModel(sa))...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *ServiceAccountResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every configurable attribute requires replacement.
	resp.Diagnostics.AddError("Update not supported", "rawtree_service_account cannot be updated in place.")
}

func (r *ServiceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServiceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	orgID, err := r.orgID(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	p := collectionPath(orgID) + "/" + url.PathEscape(state.ID.ValueString())
	if err := r.client.Global(ctx, http.MethodDelete, p, nil, nil, nil); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting service account", err.Error())
	}
}

func (r *ServiceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func toModel(sa serviceAccount) ServiceAccountModel {
	return ServiceAccountModel{
		ID:             types.StringValue(sa.ID),
		OrganizationID: types.StringValue(sa.OrganizationID),
		Name:           types.StringValue(sa.Name),
		Description:    types.StringPointerValue(sa.Description),
		Role:           types.StringValue(sa.Role),
	}
}
