package cluster

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
)

var (
	_ resource.Resource                = &ClusterResource{}
	_ resource.ResourceWithConfigure   = &ClusterResource{}
	_ resource.ResourceWithImportState = &ClusterResource{}
	_ resource.ResourceWithModifyPlan  = &ClusterResource{}
)

// Cluster creation is asynchronous: Create polls until the cluster is ready.
var (
	pollInterval  = 15 * time.Second
	createTimeout = 30 * time.Minute
)

var sizePattern = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

type ClusterResource struct {
	client *client.RawtreeClient
}

type ClusterModel struct {
	ID                 types.String    `tfsdk:"id"`
	Name               types.String    `tfsdk:"name"`
	RegionID           types.String    `tfsdk:"region_id"`
	Replicas           types.Int64     `tfsdk:"replicas"`
	MinSize            types.String    `tfsdk:"min_size"`
	MaxSize            types.String    `tfsdk:"max_size"`
	IdleTimeoutMinutes types.Int64     `tfsdk:"idle_timeout_minutes"`
	DeletionProtection types.Bool      `tfsdk:"deletion_protection"`
	S3Storage          *S3StorageModel `tfsdk:"s3_storage"`
	ExternalID         types.String    `tfsdk:"external_id"`
	ResourceTag        types.String    `tfsdk:"resource_tag"`
	Status             types.String    `tfsdk:"status"`
	DeploymentID       types.String    `tfsdk:"deployment_id"`
	CloudRegion        types.String    `tfsdk:"cloud_region"`
	RawtreeVersion     types.String    `tfsdk:"rawtree_version"`
	CreatedAt          types.String    `tfsdk:"created_at"`
}

type S3StorageModel struct {
	DataBucket    types.String `tfsdk:"data_bucket"`
	DataPath      types.String `tfsdk:"data_path"`
	BackupsBucket types.String `tfsdk:"backups_bucket"`
	BackupsPath   types.String `tfsdk:"backups_path"`
	RoleARN       types.String `tfsdk:"role_arn"`
}

// clusterItem is the API representation (ClusterItem in the OpenAPI spec).
type clusterItem struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	DeploymentID   *string `json:"deployment_id"`
	CloudRegion    *string `json:"cloud_region"`
	RegionID       *string `json:"region_id"`
	CreatedAt      string  `json:"created_at"`
	RawtreeVersion *string `json:"rawtree_version"`
	Status         struct {
		Phase   string  `json:"phase"`
		Ready   bool    `json:"ready"`
		Message *string `json:"message"`
	} `json:"status"`
	Resources *struct {
		Replicas    int64         `json:"replicas"`
		MinimumSize *resourceSize `json:"minimum_size"`
		MaximumSize *resourceSize `json:"maximum_size"`
	} `json:"resources"`
	S3Storage *struct {
		Data    s3Destination `json:"data"`
		Backups s3Destination `json:"backups"`
	} `json:"s3_storage"`
	S3StorageAccess *struct {
		ExternalID  string `json:"external_id"`
		ResourceTag string `json:"resource_tag"`
	} `json:"s3_storage_access"`
	IdleTimeoutMinutes int64 `json:"idle_timeout_minutes"`
}

type resourceSize struct {
	CPUCores    float64 `json:"cpu_cores"`
	MemoryBytes int64   `json:"memory_bytes"`
}

type s3Destination struct {
	Bucket string `json:"bucket"`
	Path   string `json:"path"`
}

type clusterSize struct {
	CPUCores  int64 `json:"cpu_cores"`
	MemoryGiB int64 `json:"memory_gib"`
}

type createRequest struct {
	Name               string      `json:"name"`
	RegionID           string      `json:"region_id"`
	Replicas           int64       `json:"replicas"`
	Size               clusterSize `json:"size"`
	IdleTimeoutMinutes *int64      `json:"idle_timeout_minutes,omitempty"`
	Autoscaling        struct {
		MinSize clusterSize `json:"min_size"`
		MaxSize clusterSize `json:"max_size"`
	} `json:"autoscaling"`
	S3Storage *struct {
		Data    s3Destination `json:"data"`
		Backups s3Destination `json:"backups"`
		RoleARN string        `json:"role_arn"`
	} `json:"s3_storage,omitempty"`
	S3StorageAccess map[string]string `json:"s3_storage_access,omitempty"`
}

func NewResource() resource.Resource {
	return &ClusterResource{}
}

func (r *ClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func resourceSchema() schema.Schema {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replaceKeep := []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}
	sizeValidators := []validator.String{stringvalidator.RegexMatches(sizePattern, "must be CPU_CORES:MEMORY_GIB, for example 2:8")}
	return schema.Schema{
		Description: "Manages a dedicated RawTree cluster in the provider organization. Creation waits until the cluster is ready (up to 30 minutes). Interactive use only: requires a user access token, which expires after 15 minutes and does not work in CI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Cluster ID.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"name": schema.StringAttribute{
				Description: "Cluster name. Can be changed in place.",
				Required:    true,
			},
			"region_id": schema.StringAttribute{
				Description: "Region to create the cluster in: its ID from `rtree cluster regions`, its name or its cloud region. A change to a different region forces a new resource.",
				Required:    true,
			},
			"replicas": schema.Int64Attribute{
				Description:   "Number of replicas.",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"min_size": schema.StringAttribute{
				Description:   "Initial and minimum size per replica as CPU_CORES:MEMORY_GIB, for example 2:8. `rtree cluster sizes` lists the valid sizes.",
				Required:      true,
				Validators:    sizeValidators,
				PlanModifiers: replace,
			},
			"max_size": schema.StringAttribute{
				Description:   "Maximum autoscaling size per replica as CPU_CORES:MEMORY_GIB. Defaults to min_size.",
				Optional:      true,
				Computed:      true,
				Validators:    sizeValidators,
				PlanModifiers: replaceKeep,
			},
			"idle_timeout_minutes": schema.Int64Attribute{
				Description:   "Minutes without activity before the cluster pauses automatically; 0 disables idling. Defaults to the API default. Can be changed in place.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"deletion_protection": schema.BoolAttribute{
				Description: "When true (the default), Terraform refuses to delete the cluster. This also blocks a replacement. Set it to false and apply before you destroy the cluster.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"s3_storage": schema.SingleNestedAttribute{
				Description: "Customer-owned S3 storage. Omit for RawTree-managed storage. A change to the buckets or paths forces a new resource.",
				Optional:    true,
				// Nested modifiers do not run when the block is added or removed.
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = req.PlanValue.IsNull() != req.StateValue.IsNull()
					}, "Replaces the cluster when s3_storage is added or removed.", "Replaces the cluster when s3_storage is added or removed.")},
				Attributes: map[string]schema.Attribute{
					"data_bucket": schema.StringAttribute{
						Description:   "S3 bucket for cluster data.",
						Required:      true,
						PlanModifiers: replace,
					},
					"data_path": schema.StringAttribute{
						Description:   "Path prefix in the data bucket.",
						Optional:      true,
						Computed:      true,
						Default:       stringdefault.StaticString(""),
						PlanModifiers: replace,
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
						Default:       stringdefault.StaticString(""),
						PlanModifiers: replace,
					},
					"role_arn": schema.StringAttribute{
						Description: "IAM role ARN that RawTree assumes to access the buckets. A change forces a new resource, except after import, when the API does not return it.",
						Required:    true,
						// The API never returns role_arn: null in state means an import, not a change.
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
							func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
								resp.RequiresReplace = !req.StateValue.IsNull()
							}, "Replaces the cluster when role_arn changes.", "Replaces the cluster when role_arn changes.")},
					},
				},
			},
			"external_id": schema.StringAttribute{
				Description:   "External ID that RawTree sends when it assumes the S3 storage role. Generated when omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceKeep,
			},
			"resource_tag": schema.StringAttribute{
				Description:   "Value of the rawtree.com/cluster AWS resource tag. Generated when omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replaceKeep,
			},
			"status": schema.StringAttribute{
				Description: "Lifecycle phase, for example ready, stopped or provisioning.",
				Computed:    true,
			},
			"deployment_id": schema.StringAttribute{
				Description:   "Regional deployment that serves the cluster.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"cloud_region": schema.StringAttribute{
				Description:   "Cloud region of the serving deployment.",
				Computed:      true,
				PlanModifiers: keep,
			},
			"rawtree_version": schema.StringAttribute{
				Description: "RawTree version the cluster runs.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description:   "Creation timestamp.",
				Computed:      true,
				PlanModifiers: keep,
			},
		},
	}
}

func (r *ClusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *ClusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan replaces the cluster only when region_id selects a different region.
// The ID, the name and the cloud region of one region are the same placement.
func (r *ClusterResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.client == nil {
		return
	}
	var plan, state types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("region_id"), &plan)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("region_id"), &state)...)
	if resp.Diagnostics.HasError() || plan.IsUnknown() || plan.Equal(state) {
		return
	}
	if planned, err := r.region(ctx, plan.ValueString()); err == nil {
		if current, err := r.region(ctx, state.ValueString()); err == nil && planned.ID == current.ID {
			return
		}
	}
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root("region_id"))
}

func (r *ClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region, err := r.region(ctx, plan.RegionID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating cluster", err.Error())
		return
	}
	// A nil deployment is the default deployment.
	dep := region.Deployment()
	if !region.Enabled {
		resp.Diagnostics.AddError("Error creating cluster", fmt.Sprintf("region %q is disabled", region.ID))
		return
	}

	if plan.MaxSize.IsUnknown() || plan.MaxSize.IsNull() {
		plan.MaxSize = plan.MinSize
	}
	body := createRequest{
		Name:     plan.Name.ValueString(),
		RegionID: region.ID,
		Replicas: plan.Replicas.ValueInt64(),
		Size:     parseSize(plan.MinSize.ValueString()),
	}
	body.Autoscaling.MinSize = body.Size
	body.Autoscaling.MaxSize = parseSize(plan.MaxSize.ValueString())
	if !plan.IdleTimeoutMinutes.IsUnknown() && !plan.IdleTimeoutMinutes.IsNull() {
		v := plan.IdleTimeoutMinutes.ValueInt64()
		body.IdleTimeoutMinutes = &v
	}
	if s := plan.S3Storage; s != nil {
		body.S3Storage = &struct {
			Data    s3Destination `json:"data"`
			Backups s3Destination `json:"backups"`
			RoleARN string        `json:"role_arn"`
		}{
			Data:    s3Destination{Bucket: s.DataBucket.ValueString(), Path: s.DataPath.ValueString()},
			Backups: s3Destination{Bucket: s.BackupsBucket.ValueString(), Path: s.BackupsPath.ValueString()},
			RoleARN: s.RoleARN.ValueString(),
		}
	}
	access := map[string]string{}
	if v := plan.ExternalID; !v.IsUnknown() && !v.IsNull() {
		access["external_id"] = v.ValueString()
	}
	if v := plan.ResourceTag; !v.IsUnknown() && !v.IsNull() {
		access["resource_tag"] = v.ValueString()
	}
	if len(access) > 0 {
		body.S3StorageAccess = access
	}

	var created clusterItem
	if err := r.client.Deployed(ctx, dep, http.MethodPost, "/v1/clusters", nil, body, &created); err != nil {
		resp.Diagnostics.AddError("Error creating cluster", err.Error())
		return
	}
	applyItem(&plan, &created)
	// Save the ID before waiting, so a failed wait leaves a tainted resource instead of an orphan.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ready, err := r.waitReady(ctx, routeOf(&created, dep), created.ID)
	if err != nil {
		// The cluster is tainted and has no data: let the replacement delete it.
		plan.DeletionProtection = types.BoolValue(false)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.AddError("Error waiting for cluster to become ready", err.Error())
		return
	}
	applyItem(&plan, ready)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dep, err := r.deployment(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading cluster", err.Error())
		return
	}
	item, err := r.get(ctx, dep, state.ID.ValueString())
	if clusterGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusPending(err) {
		// A 404 or 503 here means the status is not published, not that the cluster is deleted.
		resp.Diagnostics.AddWarning("Cluster status is not available", "The state was not refreshed: "+err.Error())
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading cluster", err.Error())
		return
	}
	applyItem(&state, item)
	importItem(&state, item)
	if state.RegionID.IsNull() {
		if item.RegionID != nil && *item.RegionID != "" {
			state.RegionID = types.StringValue(*item.RegionID)
		} else {
			state.RegionID = r.regionOf(ctx, routeOf(item, dep))
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{}
	if !plan.Name.Equal(state.Name) {
		body["name"] = plan.Name.ValueString()
	}
	if !plan.IdleTimeoutMinutes.IsUnknown() && !plan.IdleTimeoutMinutes.Equal(state.IdleTimeoutMinutes) {
		body["idle_timeout_minutes"] = plan.IdleTimeoutMinutes.ValueInt64()
	}

	dep, err := r.deployment(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating cluster", err.Error())
		return
	}
	var item *clusterItem
	if len(body) > 0 {
		item = &clusterItem{}
		err = r.client.Deployed(ctx, dep, http.MethodPatch, clusterPath(state.ID.ValueString()), nil, body, item)
	} else {
		item, err = r.get(ctx, dep, state.ID.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating cluster", err.Error())
		return
	}
	// Drop the cached route of the old name, so resources that select the cluster by name resolve it again.
	r.client.ForgetCluster(state.ID.ValueString(), state.Name.ValueString())
	applyItem(&plan, item)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Null (an import that was not applied) counts as protected.
	if state.DeletionProtection.IsNull() || state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("deletion_protection"), "Cluster is protected from deletion",
			"The deletion of a cluster destroys its data. Set `deletion_protection = false`, run `terraform apply`, then try again.")
		return
	}

	dep, err := r.deployment(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting cluster", err.Error())
		return
	}
	// The API removes the cluster from listings at once and cleans up infrastructure asynchronously.
	err = r.client.Deployed(ctx, dep, http.MethodDelete, clusterPath(state.ID.ValueString()), nil, nil, nil)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting cluster", err.Error())
		return
	}
	r.client.ForgetCluster(state.ID.ValueString(), state.Name.ValueString())
}

// ImportState accepts the cluster ID. The API rejects a name on /v1/clusters/{id}.
func (r *ClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	dep, err := r.client.ClusterDeployment(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error importing cluster", err.Error())
		return
	}
	item, err := r.get(ctx, dep, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error importing cluster", err.Error())
		return
	}
	var state ClusterModel
	applyItem(&state, item)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func clusterPath(id string) string {
	return "/v1/clusters/" + url.PathEscape(id)
}

// region finds a region of the provider organization by ID, name or cloud region.
func (r *ClusterResource) region(ctx context.Context, id string) (*client.Region, error) {
	if r.client.Organization == "" {
		return nil, errors.New("this operation needs an organization: set the provider 'organization' attribute or RAWTREE_ORG")
	}
	regions, err := r.client.Regions(ctx)
	if err != nil {
		return nil, err
	}
	for _, reg := range regions {
		if reg.ID == id || reg.Name == id || reg.CloudRegion == id {
			return &reg, nil
		}
	}
	return nil, fmt.Errorf("region %q not found in organization %q; `rtree cluster regions` lists them", id, r.client.Organization)
}

// regionOf finds the region served by a deployment, for imported clusters.
func (r *ClusterResource) regionOf(ctx context.Context, dep *client.Deployment) types.String {
	regions, err := r.client.Regions(ctx)
	if err != nil {
		return types.StringNull()
	}
	for _, reg := range regions {
		if d := reg.Deployment(); d == nil && dep == nil || d != nil && dep != nil && *d == *dep {
			return types.StringValue(reg.ID)
		}
	}
	return types.StringNull()
}

// deployment returns the deployment that serves the cluster in state.
func (r *ClusterResource) deployment(ctx context.Context, state *ClusterModel) (*client.Deployment, error) {
	if r.client.Organization == "" {
		return nil, errors.New("this operation needs an organization: set the provider 'organization' attribute or RAWTREE_ORG")
	}
	if state.DeploymentID.ValueString() != "" && state.CloudRegion.ValueString() != "" {
		return &client.Deployment{ID: state.DeploymentID.ValueString(), Region: state.CloudRegion.ValueString()}, nil
	}
	if state.RegionID.ValueString() != "" {
		region, err := r.region(ctx, state.RegionID.ValueString())
		if err != nil {
			return nil, err
		}
		if dep := region.Deployment(); dep != nil {
			return dep, nil
		}
	}
	return r.client.ClusterDeployment(ctx, state.ID.ValueString())
}

func (r *ClusterResource) get(ctx context.Context, dep *client.Deployment, id string) (*clusterItem, error) {
	var item clusterItem
	if err := r.client.Deployed(ctx, dep, http.MethodGet, clusterPath(id), nil, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// waitReady polls the cluster until it is ready, fails or createTimeout passes.
func (r *ClusterResource) waitReady(ctx context.Context, dep *client.Deployment, id string) (*clusterItem, error) {
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
	for {
		// The status publisher can be behind a new cluster: a 404 or 503 means "not yet".
		phase := "pending"
		item, err := r.get(ctx, dep, id)
		switch {
		case statusPending(err):
		case err != nil:
			return nil, err
		case item.Status.Ready:
			return item, nil
		case item.Status.Phase == "error":
			msg := item.Status.Phase
			if item.Status.Message != nil {
				msg += ": " + *item.Status.Message
			}
			return nil, fmt.Errorf("cluster %s failed to provision (%s)", id, msg)
		default:
			phase = item.Status.Phase
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cluster %s is still %s after %s: %w", id, phase, createTimeout, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// clusterGone reports the 404 that the API returns for a deleted cluster.
func clusterGone(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Message == "Cluster not found."
}

// statusPending reports a 404 or 503 for a cluster that exists but has no published status.
func statusPending(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && !clusterGone(err) &&
		(apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusServiceUnavailable)
}

// routeOf prefers the deployment the API reports for the cluster.
func routeOf(item *clusterItem, fallback *client.Deployment) *client.Deployment {
	if item.DeploymentID != nil && item.CloudRegion != nil && *item.DeploymentID != "" && *item.CloudRegion != "" {
		return &client.Deployment{ID: *item.DeploymentID, Region: *item.CloudRegion}
	}
	return fallback
}

// applyItem copies the fields the API reports back into the model.
func applyItem(m *ClusterModel, item *clusterItem) {
	m.ID = types.StringValue(item.ID)
	m.Name = keepNormalized(m.Name, item.Name, func(s string) string { return strings.ToLower(strings.TrimSpace(s)) })
	m.IdleTimeoutMinutes = types.Int64Value(item.IdleTimeoutMinutes)
	m.Status = types.StringValue(item.Status.Phase)
	m.DeploymentID = types.StringPointerValue(item.DeploymentID)
	m.CloudRegion = types.StringPointerValue(item.CloudRegion)
	m.RawtreeVersion = types.StringPointerValue(item.RawtreeVersion)
	m.CreatedAt = types.StringValue(item.CreatedAt)
	if a := item.S3StorageAccess; a != nil {
		m.ExternalID = types.StringValue(a.ExternalID)
		m.ResourceTag = types.StringValue(a.ResourceTag)
	} else {
		m.ExternalID = nullIfUnknown(m.ExternalID)
		m.ResourceTag = nullIfUnknown(m.ResourceTag)
	}
	if s := item.S3Storage; s != nil {
		// The API lowercases buckets and trims slashes from paths: a kept spelling prevents a replacement.
		prev := m.S3Storage
		if prev == nil {
			prev = &S3StorageModel{}
		}
		bucket := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
		path := func(s string) string { return strings.Trim(strings.TrimSpace(s), "/") }
		m.S3Storage = &S3StorageModel{
			DataBucket:    keepNormalized(prev.DataBucket, s.Data.Bucket, bucket),
			DataPath:      keepNormalized(prev.DataPath, s.Data.Path, path),
			BackupsBucket: keepNormalized(prev.BackupsBucket, s.Backups.Bucket, bucket),
			BackupsPath:   keepNormalized(prev.BackupsPath, s.Backups.Path, path),
			RoleARN:       prev.RoleARN,
		}
	}
	if (m.MaxSize.IsUnknown() || m.MaxSize.IsNull()) && !m.MinSize.IsNull() {
		m.MaxSize = m.MinSize
	}
}

// importItem fills replicas and sizes only when state has none. Resources
// reported by a stopped or scaling cluster must not force a replacement.
func importItem(m *ClusterModel, item *clusterItem) {
	res := item.Resources
	if res == nil {
		return
	}
	if m.Replicas.IsNull() {
		m.Replicas = types.Int64Value(res.Replicas)
	}
	if m.MinSize.IsNull() && res.MinimumSize != nil {
		m.MinSize = types.StringValue(formatSize(*res.MinimumSize))
	}
	if m.MaxSize.IsNull() && res.MaximumSize != nil {
		m.MaxSize = types.StringValue(formatSize(*res.MaximumSize))
	}
}

// keepNormalized returns the prior value when the API value is its normalised form.
func keepNormalized(prior types.String, api string, normalize func(string) string) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && normalize(prior.ValueString()) == api {
		return prior
	}
	return types.StringValue(api)
}

func nullIfUnknown(v types.String) types.String {
	if v.IsUnknown() {
		return types.StringNull()
	}
	return v
}

func parseSize(s string) clusterSize {
	cpu, mem, _ := strings.Cut(s, ":")
	c, _ := strconv.ParseInt(cpu, 10, 64)
	m, _ := strconv.ParseInt(mem, 10, 64)
	return clusterSize{CPUCores: c, MemoryGiB: m}
}

func formatSize(s resourceSize) string {
	return fmt.Sprintf("%d:%d", int64(s.CPUCores), s.MemoryBytes>>30)
}
