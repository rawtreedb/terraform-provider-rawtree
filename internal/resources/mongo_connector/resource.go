package mongo_connector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var (
	_ resource.Resource                = &MongoConnectorResource{}
	_ resource.ResourceWithImportState = &MongoConnectorResource{}
	_ resource.ResourceWithModifyPlan  = &MongoConnectorResource{}
)

type MongoConnectorResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &MongoConnectorResource{}
}

func (r *MongoConnectorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mongo_connector"
}

func (r *MongoConnectorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *MongoConnectorResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if r.client == nil {
		return
	}

	var plan MongoConnectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *MongoConnectorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *MongoConnectorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MongoConnectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := plan.Region.ValueString()
	mongoURI := plan.MongoURI.ValueString()
	mongoDatabase := plan.MongoDatabase.ValueString()

	org := r.client.Organization
	if !plan.Organization.IsNull() && !plan.Organization.IsUnknown() && plan.Organization.ValueString() != "" {
		org = plan.Organization.ValueString()
	}
	project := r.client.Project
	if !plan.Project.IsNull() && !plan.Project.IsUnknown() && plan.Project.ValueString() != "" {
		project = plan.Project.ValueString()
	}

	table := plan.Table.ValueString()
	resourceName := util.SanitizeResourceName(fmt.Sprintf("%s-%s-%s", org, project, table))

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	iamClient := iam.NewFromConfig(awsCfg)
	smClient := secretsmanager.NewFromConfig(awsCfg)
	ecsClient := ecs.NewFromConfig(awsCfg)
	cwlClient := cloudwatchlogs.NewFromConfig(awsCfg)

	state := awsResourceState{Region: region}

	// Step 1: Create Secrets Manager secret.
	secretName := fmt.Sprintf("rawtree/mongo-connector/%s", resourceName)
	secretARN, err := createSecret(ctx, smClient, secretName, mongoURI, r.client.APIKey)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create secret", err.Error())
		return
	}
	state.SecretARN = secretARN
	state.SecretName = secretName

	// Step 2: Create CloudWatch log group.
	logGroupName := fmt.Sprintf("/rawtree/mongo-connector/%s", resourceName)
	if err := createLogGroup(ctx, cwlClient, logGroupName); err != nil {
		resp.Diagnostics.AddError("Failed to create log group", err.Error())
		return
	}
	state.LogGroupName = logGroupName

	// Step 3: Create IAM execution role (for ECS to pull secrets + write logs).
	execRoleARN, execRoleName, execPolicyARN, err := createExecutionRole(ctx, iamClient, resourceName, secretARN, logGroupName, region)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create execution role", err.Error())
		return
	}
	state.ExecutionRoleARN = execRoleARN
	state.ExecutionRoleName = execRoleName
	state.ExecutionPolicyARN = execPolicyARN

	// Step 4: Create IAM task role.
	taskRoleARN, taskRoleName, taskPolicyARN, err := createTaskRole(ctx, iamClient, resourceName)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create task role", err.Error())
		return
	}
	state.IAMRoleARN = taskRoleARN
	state.IAMRoleName = taskRoleName
	state.IAMPolicyARN = taskPolicyARN

	// Wait for IAM propagation.
	tflog.Info(ctx, "Waiting for IAM role propagation")
	time.Sleep(10 * time.Second)

	// Step 5: Create ECS cluster.
	clusterName := fmt.Sprintf("rawtree-mongo-%s", resourceName)
	clusterARN, err := createCluster(ctx, ecsClient, clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ECS cluster", err.Error())
		return
	}
	state.ECSClusterARN = clusterARN
	state.ECSClusterName = clusterName

	// Step 6: Register task definition.
	taskFamily := fmt.Sprintf("rawtree-mongo-%s", resourceName)
	cfg := ecsConfig{
		ClusterName:      clusterName,
		ServiceName:      fmt.Sprintf("rawtree-mongo-%s", resourceName),
		TaskFamily:       taskFamily,
		ImageTag:         plan.ImageTag.ValueString(),
		LogGroupName:     logGroupName,
		Region:           region,
		ExecutionRoleARN: execRoleARN,
		TaskRoleARN:      taskRoleARN,
		MongoSecretARN:   secretARN,
		RawtreeSecretARN: secretARN,
		MongoDatabase:    mongoDatabase,
		Collections:      plan.Collections.ValueString(),
		TablePrefix:      plan.TablePrefix.ValueString(),
		FullDocument:     plan.FullDocument.ValueString(),
		SnapshotEnable:   plan.SnapshotEnable.ValueBool(),
		BatchMaxRows:     int32(plan.BatchMaxRows.ValueInt64()),
		FlushInterval:    plan.FlushInterval.ValueString(),
		RawtreeEndpoint:  r.client.APIURL,
		RawtreeOrg:       org,
		RawtreeProject:   project,
	}

	taskDefARN, err := registerTaskDefinition(ctx, ecsClient, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to register task definition", err.Error())
		return
	}
	state.TaskDefinitionARN = taskDefARN
	state.TaskFamily = taskFamily

	// Step 7: Create ECS service.
	serviceName := fmt.Sprintf("rawtree-mongo-%s", resourceName)
	serviceARN, err := createService(ctx, ecsClient, clusterARN, serviceName, taskDefARN)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ECS service", err.Error())
		return
	}
	state.ECSServiceName = serviceName

	// Set state.
	plan.ID = types.StringValue(resourceName)
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)
	plan.ECSClusterARN = types.StringValue(clusterARN)
	plan.ECSServiceARN = types.StringValue(serviceARN)
	plan.TaskDefinition = types.StringValue(taskDefARN)
	plan.LogGroupName = types.StringValue(logGroupName)
	plan.SecretARN = types.StringValue(secretARN)

	stateJSON, _ := json.Marshal(state)
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MongoConnectorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data MongoConnectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateJSON, diags := req.Private.GetKey(ctx, "aws_resources")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if stateJSON == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	var state awsResourceState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		resp.Diagnostics.AddError("Failed to read internal state", err.Error())
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	ecsClient := ecs.NewFromConfig(awsCfg)

	// Verify ECS service exists.
	out, err := ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  &state.ECSClusterName,
		Services: []string{state.ECSServiceName},
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to describe ECS service", err.Error())
		return
	}
	if len(out.Services) == 0 || *out.Services[0].Status == "INACTIVE" {
		resp.State.RemoveResource(ctx)
		return
	}

	data.APIURL = types.StringValue(r.client.APIURL)
	if data.Organization.IsNull() || data.Organization.ValueString() == "" {
		data.Organization = types.StringValue(r.client.Organization)
	}
	if data.Project.IsNull() || data.Project.ValueString() == "" {
		data.Project = types.StringValue(r.client.Project)
	}

	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MongoConnectorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan MongoConnectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateJSON, diags := req.Private.GetKey(ctx, "aws_resources")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state awsResourceState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		resp.Diagnostics.AddError("Failed to read internal state", err.Error())
		return
	}

	org := r.client.Organization
	if !plan.Organization.IsNull() && !plan.Organization.IsUnknown() && plan.Organization.ValueString() != "" {
		org = plan.Organization.ValueString()
	}
	project := r.client.Project
	if !plan.Project.IsNull() && !plan.Project.IsUnknown() && plan.Project.ValueString() != "" {
		project = plan.Project.ValueString()
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	smClient := secretsmanager.NewFromConfig(awsCfg)
	ecsClient := ecs.NewFromConfig(awsCfg)

	// Update secret if API key changed.
	if err := updateSecret(ctx, smClient, state.SecretARN, plan.MongoURI.ValueString(), r.client.APIKey); err != nil {
		resp.Diagnostics.AddError("Failed to update secret", err.Error())
		return
	}

	// Register new task definition with updated config.
	cfg := ecsConfig{
		ClusterName:      state.ECSClusterName,
		ServiceName:      state.ECSServiceName,
		TaskFamily:       state.TaskFamily,
		ImageTag:         plan.ImageTag.ValueString(),
		LogGroupName:     state.LogGroupName,
		Region:           state.Region,
		ExecutionRoleARN: state.ExecutionRoleARN,
		TaskRoleARN:      state.IAMRoleARN,
		MongoSecretARN:   state.SecretARN,
		RawtreeSecretARN: state.SecretARN,
		MongoDatabase:    plan.MongoDatabase.ValueString(),
		Collections:      plan.Collections.ValueString(),
		TablePrefix:      plan.TablePrefix.ValueString(),
		FullDocument:     plan.FullDocument.ValueString(),
		SnapshotEnable:   plan.SnapshotEnable.ValueBool(),
		BatchMaxRows:     int32(plan.BatchMaxRows.ValueInt64()),
		FlushInterval:    plan.FlushInterval.ValueString(),
		RawtreeEndpoint:  r.client.APIURL,
		RawtreeOrg:       org,
		RawtreeProject:   project,
	}

	taskDefARN, err := registerTaskDefinition(ctx, ecsClient, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to register updated task definition", err.Error())
		return
	}
	state.TaskDefinitionARN = taskDefARN

	// Update ECS service to use new task definition.
	if err := updateService(ctx, ecsClient, state.ECSClusterARN, state.ECSServiceName, taskDefARN); err != nil {
		resp.Diagnostics.AddError("Failed to update ECS service", err.Error())
		return
	}

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)
	plan.TaskDefinition = types.StringValue(taskDefARN)

	updatedStateJSON, _ := json.Marshal(state)
	resp.Private.SetKey(ctx, "aws_resources", updatedStateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MongoConnectorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data MongoConnectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateJSON, diags := req.Private.GetKey(ctx, "aws_resources")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if stateJSON == nil {
		return
	}

	var state awsResourceState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		resp.Diagnostics.AddError("Failed to read internal state", err.Error())
		return
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	iamClient := iam.NewFromConfig(awsCfg)
	smClient := secretsmanager.NewFromConfig(awsCfg)
	ecsClient := ecs.NewFromConfig(awsCfg)
	cwlClient := cloudwatchlogs.NewFromConfig(awsCfg)

	tflog.Info(ctx, "Deleting MongoDB connector resource", map[string]interface{}{
		"cluster":  state.ECSClusterName,
		"service":  state.ECSServiceName,
	})

	// Delete in reverse order.

	// 1. Delete ECS service.
	if err := deleteService(ctx, ecsClient, state.ECSClusterARN, state.ECSServiceName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete ECS service", err.Error())
	}

	// 2. Deregister task definitions.
	deregisterTaskDefinitions(ctx, ecsClient, state.TaskFamily)

	// 3. Delete ECS cluster.
	if err := deleteCluster(ctx, ecsClient, state.ECSClusterName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete ECS cluster", err.Error())
	}

	// 4. Delete IAM roles.
	if err := util.DeleteRole(ctx, iamClient, state.IAMRoleName, state.IAMPolicyARN, ""); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete task IAM role", err.Error())
	}
	if err := util.DeleteRole(ctx, iamClient, state.ExecutionRoleName, state.ExecutionPolicyARN, ""); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete execution IAM role", err.Error())
	}

	// 5. Delete secret.
	if err := deleteSecret(ctx, smClient, state.SecretARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete secret", err.Error())
	}

	// 6. Delete log group.
	if err := deleteLogGroup(ctx, cwlClient, state.LogGroupName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete log group", err.Error())
	}
}

func (r *MongoConnectorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"The rawtree_mongo_connector resource does not support import. "+
			"Please create the resource using Terraform.",
	)
}
