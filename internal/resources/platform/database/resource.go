package database

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.ResourceWithIdentity    = (*databaseResource)(nil)
	_ resource.ResourceWithConfigure   = (*databaseResource)(nil)
	_ resource.ResourceWithImportState = (*databaseResource)(nil)

	scopeName       = regexp.MustCompile(`^[^/[:space:]]+$`)
	scopeValidators = []validator.String{stringvalidator.RegexMatches(scopeName, "use a name without slashes or whitespace")}
	dbName          = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	reserved        = []string{"default", "system", "information_schema", "INFORMATION_SCHEMA", "rawtree"}
)

type databaseResource struct{ client *client.PlatformClient }

type databaseModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Cluster      types.String `tfsdk:"cluster"`
	Name         types.String `tfsdk:"name"`
}

func (m databaseModel) database() client.Database {
	return client.Database{Organization: m.Organization.ValueString(), Cluster: m.Cluster.ValueString(), Name: m.Name.ValueString()}
}

// Older Terraform clients can supply state without resource identity.
func (m databaseModel) setIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity) diag.Diagnostics {
	if identity == nil {
		return nil
	}
	return identity.SetAttribute(ctx, path.Root("id"), m.ID)
}

func manageableName(name string) bool {
	return dbName.MatchString(name) && !slices.Contains(reserved, name)
}

func NewResource() resource.Resource { return &databaseResource{} }

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *databaseResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{Attributes: map[string]identityschema.Attribute{
		"id": identityschema.StringAttribute{RequiredForImport: true, Description: "Database identity in organization/cluster/name format."},
	}}
}

func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a named database in an existing Tinybird RawTree cluster. Removing it deletes its data.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Database identity in organization/cluster/name format."},
			"organization": schema.StringAttribute{
				Required: true, Description: "Organization name.", Validators: scopeValidators, PlanModifiers: replace,
			},
			"cluster": schema.StringAttribute{
				Required: true, Description: "Cluster name within the organization.", Validators: scopeValidators, PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Database name. Changing it replaces the database.", PlanModifiers: replace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(dbName, "start with a letter and use up to 64 ASCII letters, digits, or underscores"),
					stringvalidator.NoneOf(reserved...),
				},
			},
		},
	}
}

func (r *databaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db := plan.database()
	// The API accepts repeated creates. Check ownership before adding an existing database to state.
	exists, err := r.client.DatabaseExists(ctx, db)
	if err != nil {
		resp.Diagnostics.AddError("Cannot check database", err.Error())
		return
	}
	if exists {
		resp.Diagnostics.AddError("Database already exists", fmt.Sprintf("Import %q to manage this database.", db.ID()))
		return
	}
	if err := r.client.CreateDatabase(ctx, db); err != nil {
		resp.Diagnostics.AddError("Cannot create database", err.Error())
		return
	}
	plan.ID = types.StringValue(db.ID())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(plan.setIdentity(ctx, resp.Identity)...)
}

func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.client.DatabaseExists(ctx, state.database())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read database", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(state.setIdentity(ctx, resp.Identity)...)
}

func (r *databaseResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected database update", "Changing the database name, organization, or cluster requires replacement.")
}

func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteDatabase(ctx, state.database()); err != nil {
		resp.Diagnostics.AddError("Cannot delete database", err.Error())
	}
}

func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	if id == "" && req.Identity != nil {
		var imported types.String
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &imported)...)
		if resp.Diagnostics.HasError() {
			return
		}
		id = imported.ValueString()
	}
	parts := strings.Split(id, "/")
	if len(parts) != 3 || !scopeName.MatchString(parts[0]) || !scopeName.MatchString(parts[1]) ||
		!manageableName(parts[2]) {
		resp.Diagnostics.AddError("Invalid database import ID", "Use organization/cluster/name for a user-managed database.")
		return
	}
	state := databaseModel{
		ID: types.StringValue(id), Organization: types.StringValue(parts[0]),
		Cluster: types.StringValue(parts[1]), Name: types.StringValue(parts[2]),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(state.setIdentity(ctx, resp.Identity)...)
}
