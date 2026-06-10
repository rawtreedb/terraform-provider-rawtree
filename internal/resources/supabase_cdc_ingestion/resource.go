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
		RawtreeSecretName:    names.RawtreeSecretName,
	}

	rawtreeSecretARN, err := util.CreateSecret(ctx, secretsClient, names.RawtreeSecretName, "Rawtree API key for Supabase CDC ingestion", cfg.APIKey)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Rawtree API key secret", err.Error())
		return
	}
	state.RawtreeSecretARN = rawtreeSecretARN

	databaseURLSecretARN := cfg.DatabaseURLSecretARN
	if cfg.DatabaseURL != "" {
		databaseURLSecretARN, err = util.CreateSecret(ctx, secretsClient, names.DatabaseURLSecretName, "Supabase direct Postgres URL for Rawtree CDC ingestion", cfg.DatabaseURL)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create database URL secret", err.Error())
			return
		}
		state.DatabaseURLSecretName = names.DatabaseURLSecretName
		state.ManagedDatabaseURLSecret = true
	}
	state.DatabaseURLSecretARN = databaseURLSecretARN

	tlsSecretARN := cfg.TLSRootCertSecretARN
	if cfg.TLSRootCertPEM != "" {
		tlsSecretARN, err = util.CreateSecret(ctx, secretsClient, names.TLSRootCertSecretName, "Supabase database CA certificate for Rawtree CDC ingestion", cfg.TLSRootCertPEM)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create TLS root certificate secret", err.Error())
			return
		}
		state.TLSRootCertSecretName = names.TLSRootCertSecretName
		state.ManagedTLSRootCertSecret = true
	}
	state.TLSRootCertSecretARN = tlsSecretARN

	secretARNs := secretARNs{
		RawtreeAPIKeyARN: rawtreeSecretARN,
		DatabaseURLARN:   databaseURLSecretARN,
		TLSRootCertARN:   tlsSecretARN,
	}

	if err := util.CreateLogGroup(ctx, logsClient, names.LogGroupName, cfg.LogRetentionDays); err != nil {
		resp.Diagnostics.AddError("Failed to create CloudWatch log group", err.Error())
		return
	}

	executionRoleARN, executionRoleName, executionPolicyARN, err := createExecutionRole(ctx, iamClient, cfg.ResourceName, compactStrings(rawtreeSecretARN, databaseURLSecretARN, tlsSecretARN))
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
		initTaskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, secretARNs, executionRoleARN, cfg.InitCommand)
		if err != nil {
			resp.Diagnostics.AddError("Failed to register initialization ECS task definition", err.Error())
			return
		}
		if err := runInitializationTask(ctx, ecsClient, cfg, clusterARN, initTaskDefinitionARN); err != nil {
			resp.Diagnostics.AddError("Failed to run initialization ECS task", err.Error())
			return
		}
		if err := deregisterTaskDefinition(ctx, ecsClient, initTaskDefinitionARN); err != nil {
			resp.Diagnostics.AddWarning("Failed to deregister initialization task definition", err.Error())
		}
	}

	taskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, secretARNs, executionRoleARN, cfg.WorkerCommand)
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

	if err := util.PutSecretValue(ctx, secretsClient, state.RawtreeSecretARN, cfg.APIKey); err != nil {
		resp.Diagnostics.AddError("Failed to update Rawtree API key secret", err.Error())
		return
	}
	if state.ManagedDatabaseURLSecret {
		if cfg.DatabaseURL == "" {
			resp.Diagnostics.AddError("Cannot Switch Managed Database URL Secret to External ARN",
				"Changing from database_url to database_url_secret_arn requires replacing the resource.")
			return
		}
		if err := util.PutSecretValue(ctx, secretsClient, state.DatabaseURLSecretARN, cfg.DatabaseURL); err != nil {
			resp.Diagnostics.AddError("Failed to update database URL secret", err.Error())
			return
		}
	} else {
		state.DatabaseURLSecretARN = cfg.DatabaseURLSecretARN
	}
	if state.ManagedTLSRootCertSecret {
		if cfg.TLSRootCertPEM == "" {
			resp.Diagnostics.AddError("Cannot Switch Managed TLS Certificate Secret to External ARN",
				"Changing from tls_root_cert_pem to tls_root_cert_secret_arn requires replacing the resource.")
			return
		}
		if err := util.PutSecretValue(ctx, secretsClient, state.TLSRootCertSecretARN, cfg.TLSRootCertPEM); err != nil {
			resp.Diagnostics.AddError("Failed to update TLS root certificate secret", err.Error())
			return
		}
	} else {
		state.TLSRootCertSecretARN = cfg.TLSRootCertSecretARN
	}

	if err := util.PutLogRetention(ctx, logsClient, state.LogGroupName, cfg.LogRetentionDays); err != nil {
		resp.Diagnostics.AddError("Failed to update CloudWatch log retention", err.Error())
		return
	}

	secretARNs := secretARNs{
		RawtreeAPIKeyARN: state.RawtreeSecretARN,
		DatabaseURLARN:   state.DatabaseURLSecretARN,
		TLSRootCertARN:   state.TLSRootCertSecretARN,
	}

	taskDefinitionARN, err := registerTaskDefinition(ctx, ecsClient, cfg, names, secretARNs, state.ExecutionRoleARN, cfg.WorkerCommand)
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

	if err := deleteService(ctx, ecsClient, state.ClusterARN, state.ServiceName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete ECS service", err.Error())
	}
	if err := deregisterTaskDefinition(ctx, ecsClient, state.TaskDefinitionARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to deregister ECS task definition", err.Error())
	}
	if err := deleteCluster(ctx, ecsClient, state.ClusterARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete ECS cluster", err.Error())
	}
	if err := util.DeleteRole(ctx, iamClient, state.ExecutionRoleName, state.ExecutionPolicyARN, ecsTaskExecutionManagedPolicyARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete ECS execution role", err.Error())
	}
	if err := util.DeleteLogGroup(ctx, logsClient, state.LogGroupName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete CloudWatch log group", err.Error())
	}
	if err := util.DeleteSecret(ctx, secretsClient, state.RawtreeSecretARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Rawtree API key secret", err.Error())
	}
	if state.ManagedDatabaseURLSecret {
		if err := util.DeleteSecret(ctx, secretsClient, state.DatabaseURLSecretARN); err != nil {
			resp.Diagnostics.AddWarning("Failed to delete database URL secret", err.Error())
		}
	}
	if state.ManagedTLSRootCertSecret {
		if err := util.DeleteSecret(ctx, secretsClient, state.TLSRootCertSecretARN); err != nil {
			resp.Diagnostics.AddWarning("Failed to delete TLS root certificate secret", err.Error())
		}
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
	plan.RawtreeSecretARN = types.StringValue(state.RawtreeSecretARN)
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

func compactStrings(values ...string) []string {
	var out []string
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
