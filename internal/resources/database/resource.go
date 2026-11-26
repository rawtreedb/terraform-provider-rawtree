package database

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &DatabaseResource{}
	_ resource.ResourceWithConfigure   = &DatabaseResource{}
	_ resource.ResourceWithImportState = &DatabaseResource{}
)

type DatabaseResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &DatabaseResource{}
}

type DatabaseModel struct {
	ID        types.String    `tfsdk:"id"`
	Name      types.String    `tfsdk:"name"`
	S3Storage *S3StorageModel `tfsdk:"s3_storage"`
}

type S3StorageModel struct {
	DataBucket    types.String `tfsdk:"data_bucket"`
	DataPath      types.String `tfsdk:"data_path"`
	BackupsBucket types.String `tfsdk:"backups_bucket"`
	BackupsPath   types.String `tfsdk:"backups_path"`
	RoleARN       types.String `tfsdk:"role_arn"`
}

type s3Destination struct {
	Bucket string `json:"bucket"`
	Path   string `json:"path"`
}

type s3StorageInput struct {
	Data    s3Destination `json:"data"`
	Backups s3Destination `json:"backups"`
	RoleARN string        `json:"role_arn"`
}

type createDatabaseRequest struct {
	Name      string          `json:"name"`
	S3Storage *s3StorageInput `json:"s3_storage,omitempty"`
}

type databaseItem struct {
	Name      string `json:"name"`
	S3Storage *struct {
		Data    s3Destination `json:"data"`
		Backups s3Destination `json:"backups"`
	} `json:"s3_storage"`
}

func (r *DatabaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *DatabaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func resourceSchema() schema.Schema {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	// The path defaults to "" on the API side; Computed keeps what the API stores.
	replaceComputed := []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}
	return schema.Schema{
		Description: "Manages a RawTree database.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Database name.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "Database name.",
				Required:      true,
				PlanModifiers: replace,
			},
			"s3_storage": schema.SingleNestedAttribute{
				Description: "Customer-owned S3 storage for the database. Tables in the database inherit it. Omit to use the cluster default storage.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"data_bucket": schema.StringAttribute{
						Description:   "S3 bucket for table data.",
						Required:      true,
						PlanModifiers: replace,
					},
					"data_path": schema.StringAttribute{
						Description:   "Path prefix in the data bucket.",
						Optional:      true,
						Computed:      true,
						PlanModifiers: replaceComputed,
					},
					"backups_bucket": schema.StringAttribute{
						Description:   "S3 bucket for backups.",
						Required:      true,
						PlanModifiers: replace,
					},
					"backups_path": schema.StringAttribute{
						Description:   "Path prefix in the backups bucket.",
						Optional:      true,
						Computed:      true,
						PlanModifiers: replaceComputed,
					},
					"role_arn": schema.StringAttribute{
						Description: "IAM role ARN that RawTree assumes to access the buckets. The API does not return it, so it is empty after import and setting it then does not replace the database.",
						Required:    true,
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
							func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
								resp.RequiresReplace = !req.StateValue.IsNull()
							},
							"Changing the role ARN replaces the database, unless the state has none (after import).",
							"Changing the role ARN replaces the database, unless the state has none (after import).",
						)},
					},
				},
			},
		},
	}
}

func (r *DatabaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DatabaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API creates with IF NOT EXISTS: without this check, Terraform would adopt and later drop a database it did not create.
	existing, err := r.find(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating database", err.Error())
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError("Database already exists",
			fmt.Sprintf("Database %q already exists. Import it with `terraform import`, or choose a different name.", plan.Name.ValueString()))
		return
	}

	body := createDatabaseRequest{Name: plan.Name.ValueString()}
	if s := plan.S3Storage; s != nil {
		body.S3Storage = &s3StorageInput{
			Data:    s3Destination{Bucket: s.DataBucket.ValueString(), Path: s.DataPath.ValueString()},
			Backups: s3Destination{Bucket: s.BackupsBucket.ValueString(), Path: s.BackupsPath.ValueString()},
			RoleARN: s.RoleARN.ValueString(),
		}
	}
	var out struct {
		Database struct {
			Name string `json:"name"`
		} `json:"database"`
	}
	if err := r.client.Data(ctx, http.MethodPost, "/v1/databases", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Error creating database", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.Name.ValueString())

	// Unset paths are computed: read back what the API stored.
	if s := plan.S3Storage; s != nil && (s.DataPath.IsUnknown() || s.BackupsPath.IsUnknown()) {
		item, err := r.find(ctx, plan.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error reading database", err.Error())
			return
		}
		dataPath, backupsPath := "", ""
		if item != nil && item.S3Storage != nil {
			dataPath, backupsPath = item.S3Storage.Data.Path, item.S3Storage.Backups.Path
		}
		if s.DataPath.IsUnknown() {
			s.DataPath = types.StringValue(dataPath)
		}
		if s.BackupsPath.IsUnknown() {
			s.BackupsPath = types.StringValue(backupsPath)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DatabaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	item, err := r.find(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading database", err.Error())
		return
	}
	if item == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	applyItem(&state, item)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only runs when role_arn is set after import; it has nothing to send.
func (r *DatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DatabaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DatabaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Data(ctx, http.MethodDelete, "/v1/databases/"+url.PathEscape(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting database", err.Error())
	}
}

func (r *DatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// find lists the databases and returns the named one, or nil.
func (r *DatabaseResource) find(ctx context.Context, name string) (*databaseItem, error) {
	var out struct {
		Databases []databaseItem `json:"databases"`
	}
	if err := r.client.Data(ctx, http.MethodGet, "/v1/databases", nil, nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Databases {
		if out.Databases[i].Name == name {
			return &out.Databases[i], nil
		}
	}
	return nil, nil
}

// applyItem copies the API view into the model. role_arn is not returned, so
// it is kept from the model. Paths that only differ in leading or trailing
// slashes keep the model value, so a normalised path does not replace the
// database.
func applyItem(m *DatabaseModel, item *databaseItem) {
	m.ID = types.StringValue(item.Name)
	m.Name = types.StringValue(item.Name)
	if item.S3Storage == nil {
		m.S3Storage = nil
		return
	}
	prev := m.S3Storage
	if prev == nil {
		prev = &S3StorageModel{RoleARN: types.StringNull()}
	}
	m.S3Storage = &S3StorageModel{
		DataBucket:    keepBucket(prev.DataBucket, item.S3Storage.Data.Bucket),
		DataPath:      keepPath(prev.DataPath, item.S3Storage.Data.Path),
		BackupsBucket: keepBucket(prev.BackupsBucket, item.S3Storage.Backups.Bucket),
		BackupsPath:   keepPath(prev.BackupsPath, item.S3Storage.Backups.Path),
		RoleARN:       prev.RoleARN,
	}
}

// keepBucket keeps the configured bucket when the API value is its trimmed, lowercase form.
func keepBucket(prev types.String, api string) types.String {
	if !prev.IsNull() && !prev.IsUnknown() && strings.ToLower(strings.TrimSpace(prev.ValueString())) == api {
		return prev
	}
	return types.StringValue(api)
}

func keepPath(prev types.String, api string) types.String {
	if !prev.IsNull() && !prev.IsUnknown() && strings.Trim(prev.ValueString(), "/") == strings.Trim(api, "/") {
		return prev
	}
	return types.StringValue(api)
}
