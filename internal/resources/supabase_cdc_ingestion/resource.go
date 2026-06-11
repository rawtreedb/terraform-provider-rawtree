package supabase_cdc_ingestion

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var (
	_ resource.Resource                = &SupabaseCDCIngestionResource{}
	_ resource.ResourceWithImportState = &SupabaseCDCIngestionResource{}
	_ resource.ResourceWithModifyPlan  = &SupabaseCDCIngestionResource{}
)

type SupabaseCDCIngestionResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &SupabaseCDCIngestionResource{}
}

func (r *SupabaseCDCIngestionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_supabase_cdc_ingestion"
}

func (r *SupabaseCDCIngestionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *SupabaseCDCIngestionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SupabaseCDCIngestionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.client == nil {
		return
	}

	var plan SupabaseCDCIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org := r.client.Organization
	if stringSet(plan.Organization) {
		org = plan.Organization.ValueString()
	}
	project := r.client.Project
	if stringSet(plan.Project) {
		project = plan.Project.ValueString()
	}

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *SupabaseCDCIngestionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SupabaseCDCIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, diags := resolveConfig(ctx, &plan, r.client)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to load AWS config: %s", err))
		return
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	iamClient := iam.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)
	secretsClient := secretsmanager.NewFromConfig(awsCfg)

	names := namesFor(cfg.ResourceName)
	state := awsResourceState{
		Region:               cfg.Region,
		ResourceName:         cfg.ResourceName,
		ClusterName:          names.ClusterName,
		ServiceName:          names.ServiceName,
		TaskDefinitionFamily: names.TaskDefinitionFamily,
		LogGroupName:         names.LogGroupName,
		ConfigSecretName:     names.ConfigSecretName,
	}

	// If anything below this point fails, roll back what's been created so
	// far. Terraform doesn't persist state on a failed Create, so without this
	// the secret/log group/role/cluster/etc. would be orphaned in AWS and
	// block re-runs because of duplicate-name errors. Each cleanup step is
	// best-effort and runs even if earlier ones fail.
	defer func() {
		if !resp.Diagnostics.HasError() {
			return
		}
		var cleanup diag.Diagnostics
		// preserveLogGroup=true: keep CloudWatch logs around so you can see
		// why the failed step (init task, service create, etc.) failed.
		destroyAWSResources(ctx, state, ecsClient, iamClient, logsClient, secretsClient, true, &cleanup)
		resp.Diagnostics.Append(cleanup...)
	}()

	configJSON, err := buildConfigSecretJSON(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build managed config secret payload", err.Error())
		return
	}
	configSecretARN, err := util.CreateSecret(
		ctx, secretsClient, names.ConfigSecretName,
		"Rawtree managed config (API key + optional Supabase URL/CA) for Supabase CDC ingestion",
		configJSON,
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create managed config secret", err.Error())
		return
	}
	state.ConfigSecretARN = configSecretARN

	refs := buildECSSecretRefs(cfg, configSecretARN)
	iamSecretARNs := collectSecretARNs(cfg, configSecretARN)

	if err := util.CreateLogGroup(ctx, logsClient, names.LogGroupName, cfg.LogRetentionDays); err != nil {
		resp.Diagnostics.AddError("Failed to create CloudWatch log group", err.Error())
		return
	}

	executionRoleARN, executionRoleName, executionPolicyARN, err := createExecutionRole(ctx, iamClient, cfg.ResourceName, iamSecretARNs)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ECS execution role", err.Error())
		return
	}
	state.ExecutionRoleARN = executionRoleARN
	state.ExecutionRoleName = executionRoleName
	state.ExecutionPolicyARN = executionPolicyARN

	clusterARN, err := createCluster(ctx, ecsClient, names.ClusterName)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ECS cluster", err.Error())
		return
	}
	state.ClusterARN = clusterARN

	if cfg.RunInitializationTask {
		initTaskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, refs, executionRoleARN, cfg.InitCommand)
		if err != nil {
			resp.Diagnostics.AddError("Failed to register initialization ECS task definition", err.Error())
			return
		}
		// Track in state so the rollback path can deregister it if the init
		// run below fails. Cleared after deregister succeeds.
		state.InitTaskDefinitionARN = initTaskDefinitionARN
		if err := runInitializationTask(ctx, ecsClient, cfg, clusterARN, initTaskDefinitionARN); err != nil {
			resp.Diagnostics.AddError("Failed to run initialization ECS task", err.Error())
			return
		}
		if err := deregisterTaskDefinition(ctx, ecsClient, initTaskDefinitionARN); err != nil {
			resp.Diagnostics.AddWarning("Failed to deregister initialization task definition", err.Error())
		}
		state.InitTaskDefinitionARN = ""
	}

	taskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, refs, executionRoleARN, cfg.WorkerCommand)
	if err != nil {
		resp.Diagnostics.AddError("Failed to register ECS task definition", err.Error())
		return
	}
	state.TaskDefinitionARN = taskDefinitionARN

	serviceARN, err := createService(ctx, ecsClient, cfg, names, clusterARN, taskDefinitionARN)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ECS service", err.Error())
		return
	}
	state.ServiceARN = serviceARN

	tflog.Info(ctx, "Created Supabase CDC ingestion resource", map[string]interface{}{
		"cluster": names.ClusterName,
		"service": names.ServiceName,
	})

	setComputedValues(&plan, cfg, state)
	resp.Diagnostics.Append(setPrivateState(ctx, resp.Private, state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SupabaseCDCIngestionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data SupabaseCDCIngestionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, stateJSON, ok := readPrivateState(ctx, req.Private, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	exists, err := serviceExists(ctx, ecsClient, state.ClusterARN, state.ServiceName)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read ECS service", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	data.APIURL = types.StringValue(r.client.APIURL)
	data.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	if data.Organization.IsNull() || data.Organization.ValueString() == "" {
		data.Organization = types.StringValue(r.client.Organization)
	}
	if data.Project.IsNull() || data.Project.ValueString() == "" {
		data.Project = types.StringValue(r.client.Project)
	}

	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SupabaseCDCIngestionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SupabaseCDCIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, _, ok := readPrivateState(ctx, req.Private, &resp.Diagnostics)
	if resp.Diagnostics.HasError() || !ok {
		resp.Diagnostics.AddError("Missing Internal State", "Cannot update because private AWS resource state is missing.")
		return
	}

	cfg, diags := resolveConfig(ctx, &plan, r.client)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)
	secretsClient := secretsmanager.NewFromConfig(awsCfg)
	names := namesFor(state.ResourceName)

	// inline ↔ external switches for database_url / tls_root_cert_pem are
	// gated by RequiresReplace in the schema, so by the time Update runs the
	// "managed inline" decision is stable. Just regenerate the JSON payload
	// from cfg and overwrite the single managed secret in one call.
	configJSON, err := buildConfigSecretJSON(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build managed config secret payload", err.Error())
		return
	}
	if err := util.PutSecretValue(ctx, secretsClient, state.ConfigSecretARN, configJSON); err != nil {
		resp.Diagnostics.AddError("Failed to update managed config secret", err.Error())
		return
	}

	if err := util.PutLogRetention(ctx, logsClient, state.LogGroupName, cfg.LogRetentionDays); err != nil {
		resp.Diagnostics.AddError("Failed to update CloudWatch log retention", err.Error())
		return
	}

	refs := buildECSSecretRefs(cfg, state.ConfigSecretARN)

	taskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, refs, state.ExecutionRoleARN, cfg.WorkerCommand)
	if err != nil {
		resp.Diagnostics.AddError("Failed to register ECS task definition", err.Error())
		return
	}
	oldTaskDefinitionARN := state.TaskDefinitionARN
	state.TaskDefinitionARN = taskDefinitionARN

	if err := updateService(ctx, ecsClient, cfg, state.ClusterARN, state.ServiceName, taskDefinitionARN); err != nil {
		resp.Diagnostics.AddError("Failed to update ECS service", err.Error())
		return
	}
	if oldTaskDefinitionARN != "" && oldTaskDefinitionARN != taskDefinitionARN {
		if err := deregisterTaskDefinition(ctx, ecsClient, oldTaskDefinitionARN); err != nil {
			resp.Diagnostics.AddWarning("Failed to deregister previous task definition", err.Error())
		}
	}

	setComputedValues(&plan, cfg, state)
	resp.Diagnostics.Append(setPrivateState(ctx, resp.Private, state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SupabaseCDCIngestionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	state, _, ok := readPrivateState(ctx, req.Private, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !ok {
		resp.Diagnostics.AddError(
			"Missing Internal State",
			"Cannot destroy because private AWS resource state is missing. "+
				"ECS services, IAM roles, CloudWatch log groups, and Secrets Manager secrets may still exist in AWS. "+
				"Remove them manually, then use `terraform state rm` to drop this resource from state.",
		)
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	ecsClient := ecs.NewFromConfig(awsCfg)
	iamClient := iam.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)
	secretsClient := secretsmanager.NewFromConfig(awsCfg)

	// preserveLogGroup=false: a successful destroy should leave nothing behind.
	destroyAWSResources(ctx, state, ecsClient, iamClient, logsClient, secretsClient, false, &resp.Diagnostics)
}

// destroyAWSResources tears down every AWS resource referenced by state. Safe
// to call with a partial awsResourceState — each helper short-circuits on
// empty ARNs/names, so it's both the Delete path and the rollback path used
// when Create fails halfway through. Errors surface as warnings and don't
// abort the remaining steps: we want to remove as much as possible even when
// one helper fails.
//
// preserveLogGroup=true is passed by the rollback-on-Create-failure path so
// the worker's CloudWatch logs survive for debugging. Delete passes false so
// a clean teardown removes everything. The trade-off: failed-create runs
// leave behind a (small, empty) orphan log group — preferable to losing the
// only diagnostic for "init task did not finish".
func destroyAWSResources(
	ctx context.Context,
	state awsResourceState,
	ecsClient *ecs.Client,
	iamClient *iam.Client,
	logsClient *cloudwatchlogs.Client,
	secretsClient *secretsmanager.Client,
	preserveLogGroup bool,
	diags *diag.Diagnostics,
) {
	if err := deleteService(ctx, ecsClient, state.ClusterARN, state.ServiceName); err != nil {
		diags.AddWarning("Failed to delete ECS service", err.Error())
	}
	if err := deregisterTaskDefinition(ctx, ecsClient, state.TaskDefinitionARN); err != nil {
		diags.AddWarning("Failed to deregister ECS task definition", err.Error())
	}
	if state.InitTaskDefinitionARN != "" {
		if err := deregisterTaskDefinition(ctx, ecsClient, state.InitTaskDefinitionARN); err != nil {
			diags.AddWarning("Failed to deregister initialization ECS task definition", err.Error())
		}
	}
	if err := deleteCluster(ctx, ecsClient, state.ClusterARN); err != nil {
		diags.AddWarning("Failed to delete ECS cluster", err.Error())
	}
	if state.ExecutionRoleName != "" {
		if err := util.DeleteRole(ctx, iamClient, state.ExecutionRoleName, state.ExecutionPolicyARN, ecsTaskExecutionManagedPolicyARN); err != nil {
			diags.AddWarning("Failed to delete ECS execution role", err.Error())
		}
	}
	if preserveLogGroup {
		if state.LogGroupName != "" {
			diags.AddWarning(
				"Preserving CloudWatch log group for debugging",
				fmt.Sprintf("Resource creation failed; log group %s was left in place so you can inspect the worker's stdout/stderr. Delete it manually with `aws logs delete-log-group --log-group-name %s` after you're done investigating.",
					state.LogGroupName, state.LogGroupName),
			)
		}
	} else if err := util.DeleteLogGroup(ctx, logsClient, state.LogGroupName); err != nil {
		diags.AddWarning("Failed to delete CloudWatch log group", err.Error())
	}
	if err := util.DeleteSecret(ctx, secretsClient, state.ConfigSecretARN); err != nil {
		diags.AddWarning("Failed to delete managed config secret", err.Error())
	}
}

func (r *SupabaseCDCIngestionResource) ImportState(_ context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"The rawtree_supabase_cdc_ingestion resource does not support import. Please create the resource using Terraform.",
	)
}

func setComputedValues(plan *SupabaseCDCIngestionModel, cfg resolvedConfig, state awsResourceState) {
	plan.InitializationCommand = stringListValue(cfg.InitCommand)
	plan.WorkerCommand = stringListValue(cfg.WorkerCommand)
	plan.ID = types.StringValue(cfg.ResourceName)
	plan.APIURL = types.StringValue(cfg.APIURL)
	plan.APIKeyHash = types.StringValue(cfg.APIKeyHash)
	plan.Organization = types.StringValue(cfg.Organization)
	plan.Project = types.StringValue(cfg.Project)
	plan.ClusterARN = types.StringValue(state.ClusterARN)
	plan.ServiceARN = types.StringValue(state.ServiceARN)
	plan.TaskDefinitionARN = types.StringValue(state.TaskDefinitionARN)
	plan.LogGroupName = types.StringValue(state.LogGroupName)
	plan.ExecutionRoleARN = types.StringValue(state.ExecutionRoleARN)
	plan.ConfigSecretARN = types.StringValue(state.ConfigSecretARN)
}

func stringListValue(values []string) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.ListValueMust(types.StringType, elements)
}

type privateStateWriter interface {
	SetKey(context.Context, string, []byte) diag.Diagnostics
}

type privateStateReader interface {
	GetKey(context.Context, string) ([]byte, diag.Diagnostics)
}

func setPrivateState(ctx context.Context, private privateStateWriter, state awsResourceState) diag.Diagnostics {
	stateJSON, _ := json.Marshal(state)
	return private.SetKey(ctx, "aws_resources", stateJSON)
}

func readPrivateState(ctx context.Context, private privateStateReader, diags *diag.Diagnostics) (awsResourceState, []byte, bool) {
	stateJSON, readDiags := private.GetKey(ctx, "aws_resources")
	diags.Append(readDiags...)
	if diags.HasError() || stateJSON == nil {
		return awsResourceState{}, nil, false
	}
	var state awsResourceState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		diags.AddError("Failed to read internal state", err.Error())
		return awsResourceState{}, nil, false
	}
	return state, stateJSON, true
}

