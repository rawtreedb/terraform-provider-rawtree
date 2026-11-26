package api_key

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &APIKeyResource{}
	_ resource.ResourceWithConfigure   = &APIKeyResource{}
	_ resource.ResourceWithImportState = &APIKeyResource{}
)

type APIKeyResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &APIKeyResource{}
}

type APIKeyModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Database      types.String `tfsdk:"database"`
	Permission    types.String `tfsdk:"permission"`
	DatabaseRoles types.Set    `tfsdk:"database_roles"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	PrincipalType types.String `tfsdk:"principal_type"`
	PrincipalID   types.String `tfsdk:"principal_id"`
	Token         types.String `tfsdk:"token"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

// apiKey is the shared part of the create response and the list items.
type apiKey struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Token         string   `json:"token"`
	Permission    *string  `json:"permission"`
	DatabaseRoles []string `json:"database_roles"`
	ExpiresAt     *string  `json:"expires_at"`
	PrincipalType *string  `json:"principal_type"`
	PrincipalID   *string  `json:"principal_id"`
	CreatedAt     string   `json:"created_at"`
	Database      struct {
		Name string `json:"name"`
	} `json:"database"`
}

type createAPIKeyRequest struct {
	Name          string   `json:"name"`
	Permission    *string  `json:"permission,omitempty"`
	DatabaseRoles []string `json:"database_roles,omitempty"`
	ExpiresAt     *string  `json:"expires_at,omitempty"`
	PrincipalType *string  `json:"principal_type,omitempty"`
	PrincipalID   *string  `json:"principal_id,omitempty"`
}

func (r *APIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *APIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func resourceSchema() schema.Schema {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceComputed := []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a RawTree API key. Keys are immutable: any change creates a new key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "API key UUID.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "API key name.",
				Required:      true,
				PlanModifiers: replace,
			},
			"database": schema.StringAttribute{
				Description:   "Default database stored on the key. Defaults to the provider `database`, then to the API default (`default`).",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceComputed,
			},
			"permission": schema.StringAttribute{
				Description:   "Permission level: `admin`, `read_write`, `write_only` or `read_only`. Exactly one of `permission` and `database_roles` is required.",
				Optional:      true,
				PlanModifiers: replace,
				Validators: []validator.String{
					stringvalidator.OneOf("admin", "read_write", "write_only", "read_only"),
					stringvalidator.ExactlyOneOf(path.MatchRoot("permission"), path.MatchRoot("database_roles")),
				},
			},
			"database_roles": schema.SetAttribute{
				Description:   "Existing ClickHouse roles the key acts with. Requires administrator access.",
				Optional:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
				Validators:    []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"expires_at": schema.StringAttribute{
				Description:   "Expiration instant in RFC 3339 format. Omit for a key that never expires.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"principal_type": schema.StringAttribute{
				Description:   "Identity the key acts as: `user` or `service_account`. Defaults to the caller. Set together with `principal_id`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceComputed,
				Validators: []validator.String{
					stringvalidator.OneOf("user", "service_account"),
					stringvalidator.AlsoRequires(path.MatchRoot("principal_id")),
				},
			},
			"principal_id": schema.StringAttribute{
				Description:   "UUID of the user or service account the key acts as. Set together with `principal_type`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceComputed,
				Validators:    []validator.String{stringvalidator.AlsoRequires(path.MatchRoot("principal_type"))},
			},
			"token": schema.StringAttribute{
				Description:   "The API key secret (`rt_...`). Only returned on create, so it is null after import.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Description:   "Creation time.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *APIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *APIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan APIKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := createAPIKeyRequest{
		Name:          plan.Name.ValueString(),
		Permission:    plan.Permission.ValueStringPointer(),
		ExpiresAt:     plan.ExpiresAt.ValueStringPointer(),
		PrincipalType: knownPointer(plan.PrincipalType),
		PrincipalID:   knownPointer(plan.PrincipalID),
	}
	if !plan.DatabaseRoles.IsNull() {
		resp.Diagnostics.Append(plan.DatabaseRoles.ElementsAs(ctx, &body.DatabaseRoles, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	var query url.Values
	database := r.client.Database
	if p := knownPointer(plan.Database); p != nil {
		database = *p
	}
	if database != "" {
		query = url.Values{"database": {database}}
	}

	var out apiKey
	if err := r.client.Data(ctx, http.MethodPost, "/v1/api-keys", query, body, &out); err != nil {
		resp.Diagnostics.AddError("Error creating API key", err.Error())
		return
	}
	plan.Token = types.StringValue(out.Token)
	// The create response has no created_at.
	plan.CreatedAt = types.StringNull()
	applyKey(ctx, &plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Fill created_at from the list; the key exists even if this fails.
	key, err := r.find(ctx, out.ID)
	if err != nil {
		resp.Diagnostics.AddWarning("Error reading API key", err.Error())
		return
	}
	if key != nil {
		plan.CreatedAt = types.StringValue(key.CreatedAt)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *APIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state APIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.find(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading API key", err.Error())
		return
	}
	if key == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	// The list token is never written to state: only the create response has the secret.
	applyKey(ctx, &state, key)
	state.CreatedAt = types.StringValue(key.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every attribute requires replacement.
func (r *APIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("API keys are immutable", "Every API key attribute requires replacement.")
}

func (r *APIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state APIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Data(ctx, http.MethodDelete, "/v1/api-keys/"+url.PathEscape(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting API key", err.Error())
	}
}

func (r *APIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// find lists the cluster's API keys and returns the one with the ID, or nil.
func (r *APIKeyResource) find(ctx context.Context, id string) (*apiKey, error) {
	var out struct {
		Keys []apiKey `json:"keys"`
	}
	if err := r.client.Data(ctx, http.MethodGet, "/v1/api-keys", nil, nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Keys {
		if out.Keys[i].ID == id {
			return &out.Keys[i], nil
		}
	}
	return nil, nil
}

// applyKey copies the API view into the model, except the token.
func applyKey(ctx context.Context, m *APIKeyModel, k *apiKey) {
	m.ID = types.StringValue(k.ID)
	m.Name = types.StringValue(k.Name)
	m.Database = types.StringValue(k.Database.Name)
	m.Permission = types.StringPointerValue(k.Permission)
	m.PrincipalType = types.StringPointerValue(k.PrincipalType)
	m.PrincipalID = types.StringPointerValue(k.PrincipalID)
	if len(k.DatabaseRoles) == 0 {
		m.DatabaseRoles = types.SetNull(types.StringType)
	} else {
		m.DatabaseRoles, _ = types.SetValueFrom(ctx, types.StringType, k.DatabaseRoles)
	}
	// The API returns UTC; keep the configured form while it is the same instant.
	if !sameInstant(m.ExpiresAt, k.ExpiresAt) {
		m.ExpiresAt = types.StringPointerValue(k.ExpiresAt)
	}
}

func sameInstant(configured types.String, api *string) bool {
	if configured.IsNull() || configured.IsUnknown() || api == nil {
		return (configured.IsNull() || configured.IsUnknown()) && api == nil
	}
	a, errA := time.Parse(time.RFC3339, configured.ValueString())
	b, errB := time.Parse(time.RFC3339, *api)
	if errA != nil || errB != nil {
		return configured.ValueString() == *api
	}
	return a.Equal(b)
}

func knownPointer(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}
