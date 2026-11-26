package federation_issuer

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
	_ resource.Resource                   = &FederationIssuerResource{}
	_ resource.ResourceWithConfigure      = &FederationIssuerResource{}
	_ resource.ResourceWithImportState    = &FederationIssuerResource{}
	_ resource.ResourceWithValidateConfig = &FederationIssuerResource{}
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type FederationIssuerResource struct {
	client *client.RawtreeClient
}

type FederationIssuerModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	IssuerURL      types.String `tfsdk:"issuer_url"`
	JwksSource     types.String `tfsdk:"jwks_source"`
	JwksURL        types.String `tfsdk:"jwks_url"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

// issuerConfiguration is the create and replace body (IssuerConfiguration).
// PUT replaces the whole configuration, so nulls are sent explicitly.
type issuerConfiguration struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IssuerURL   string  `json:"issuer_url"`
	JwksSource  string  `json:"jwks_source"`
	JwksURL     *string `json:"jwks_url"`
}

// issuer is the API representation (WorkloadFederationIssuer).
type issuer struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	IssuerURL      string  `json:"issuer_url"`
	JwksSource     string  `json:"jwks_source"`
	JwksURL        *string `json:"jwks_url"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func NewResource() resource.Resource {
	return &FederationIssuerResource{}
}

func (r *FederationIssuerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_issuer"
}

func resourceSchema() schema.Schema {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a workload federation issuer (an external OIDC identity provider) in the provider organization. Saving an issuer makes no network requests to it. Interactive use only: requires a user access token, which expires after 15 minutes and does not work in CI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Issuer ID. Use it as issuer_id in a rawtree_federation_rule.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"organization_id": schema.StringAttribute{
				Description:   "ID of the organization that owns the issuer.",
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
			"issuer_url": schema.StringAttribute{
				Description: "Exact expected token issuer (iss claim), an HTTPS URL without credentials, query or fragment. Stored without normalization, so a trailing slash matters.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 2048)},
			},
			"jwks_source": schema.StringAttribute{
				Description: "Where RawTree finds the signing keys: discovery (default, OpenID discovery from issuer_url) or explicit_url (jwks_url).",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("discovery"),
				Validators:  []validator.String{stringvalidator.OneOf("discovery", "explicit_url")},
			},
			"jwks_url": schema.StringAttribute{
				Description: "HTTPS JWKS URL. Required when jwks_source is explicit_url, not allowed with discovery.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 2048)},
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

func (r *FederationIssuerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *FederationIssuerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg FederationIssuerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.JwksSource.IsUnknown() || cfg.JwksURL.IsUnknown() {
		return
	}
	explicit := cfg.JwksSource.ValueString() == "explicit_url"
	if explicit && cfg.JwksURL.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("jwks_url"), "Missing jwks_url", "jwks_url is required when jwks_source is explicit_url.")
	}
	if !explicit && !cfg.JwksURL.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("jwks_url"), "Unexpected jwks_url", "Set jwks_source = \"explicit_url\" to use jwks_url.")
	}
}

func (r *FederationIssuerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// itemPath prefers the organization stored in state, so a changed provider
// organization does not point Read or Delete at another organization.
func (r *FederationIssuerResource) itemPath(ctx context.Context, m FederationIssuerModel) (string, error) {
	orgID := m.OrganizationID.ValueString()
	if orgID == "" {
		var err error
		if orgID, err = r.client.OrganizationID(ctx); err != nil {
			return "", err
		}
	}
	p := fmt.Sprintf("/v1/organizations/%s/federation-issuers", url.PathEscape(orgID))
	if id := m.ID.ValueString(); id != "" {
		p += "/" + url.PathEscape(id)
	}
	return p, nil
}

func (r *FederationIssuerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederationIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringNull()
	plan.OrganizationID = types.StringNull()
	p, err := r.itemPath(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	var out issuer
	if err := r.client.Global(ctx, http.MethodPost, p, nil, toConfiguration(plan), &out); err != nil {
		resp.Diagnostics.AddError("Error creating federation issuer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(out))...)
}

func (r *FederationIssuerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederationIssuerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.itemPath(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	var out issuer
	if err := r.client.Global(ctx, http.MethodGet, p, nil, nil, &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading federation issuer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(out))...)
}

func (r *FederationIssuerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state FederationIssuerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.itemPath(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	var out issuer
	if err := r.client.Global(ctx, http.MethodPut, p, nil, toConfiguration(plan), &out); err != nil {
		resp.Diagnostics.AddError("Error updating federation issuer", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toModel(out))...)
}

func (r *FederationIssuerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FederationIssuerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.itemPath(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Error resolving organization", err.Error())
		return
	}
	// Rules that reference the issuer must be deleted first.
	if err := r.client.Global(ctx, http.MethodDelete, p, nil, nil, nil); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting federation issuer", err.Error())
	}
}

func (r *FederationIssuerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func toConfiguration(m FederationIssuerModel) issuerConfiguration {
	return issuerConfiguration{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueStringPointer(),
		IssuerURL:   m.IssuerURL.ValueString(),
		JwksSource:  m.JwksSource.ValueString(),
		JwksURL:     m.JwksURL.ValueStringPointer(),
	}
}

func toModel(i issuer) FederationIssuerModel {
	return FederationIssuerModel{
		ID:             types.StringValue(i.ID),
		OrganizationID: types.StringValue(i.OrganizationID),
		Name:           types.StringValue(i.Name),
		Description:    types.StringPointerValue(i.Description),
		IssuerURL:      types.StringValue(i.IssuerURL),
		JwksSource:     types.StringValue(i.JwksSource),
		JwksURL:        types.StringPointerValue(i.JwksURL),
		CreatedAt:      types.StringValue(i.CreatedAt),
		UpdatedAt:      types.StringValue(i.UpdatedAt),
	}
}
