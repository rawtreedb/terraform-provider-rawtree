package s3_ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var (
	_ resource.Resource                = &S3IngestionResource{}
	_ resource.ResourceWithImportState = &S3IngestionResource{}
	_ resource.ResourceWithModifyPlan  = &S3IngestionResource{}
)

type S3IngestionResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &S3IngestionResource{}
}

func (r *S3IngestionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_s3_ingestion"
}

func (r *S3IngestionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *S3IngestionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.RawtreeClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.RawtreeClient, got: %T", req.ProviderData),
		)
		return
	}
	r.client = client
	// The pipeline sends data with the API key; an access token expires and cannot ingest.
	if client.APIKey == "" {
		resp.Diagnostics.AddError("Missing RawTree API key",
			"This resource needs the provider 'api_key' attribute (or RAWTREE_API_KEY). An access token cannot be used for ingestion.")
	}
}

// ModifyPlan sets the provider-derived attributes from the current provider
// config, so a changed API URL, API key or database triggers an Update of the
// Lambda function. Update does not change the Glue job.
func (r *S3IngestionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.client == nil {
		return
	}

	var plan S3IngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var configDatabase types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("database"), &configDatabase)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Database = types.StringValue(r.client.DatabaseOr(configDatabase.ValueString()))
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *S3IngestionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan S3IngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := plan.Region.ValueString()
	bucket := plan.Bucket.ValueString()
	prefix := plan.Prefix.ValueString()
	table := plan.Table.ValueString()
	format := plan.Format.ValueString()
	filePattern := plan.FilePattern.ValueString()

	database := r.client.DatabaseOr(plan.Database.ValueString())

	// Generate unique resource name (S3-safe: lowercase, alphanumeric, hyphens, max 40 chars).
	// The API key selects the cluster: the same database and table in two clusters must not share AWS names.
	resourceName := util.SanitizeResourceName(database+"-"+table, util.HashString(r.client.APIKey))

	// Initialize AWS clients.
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to load AWS config: %s", err))
		return
	}

	iamClient := iam.NewFromConfig(awsCfg)
	glueClient := glue.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg)
	lambdaClient := lambda.NewFromConfig(awsCfg)
	ebClient := eventbridge.NewFromConfig(awsCfg)

	state := awsResourceState{Region: region}

	// Store the Glue script in the source bucket under .rawtree/ prefix.
	// This avoids creating a separate bucket and ensures the Glue role already has access.
	scriptBucket := bucket
	scriptKey := fmt.Sprintf(".rawtree/glue-scripts/%s/glue_job.py", resourceName)
	state.ScriptBucket = scriptBucket
	state.ScriptKey = scriptKey

	// Step 1: Create IAM roles.
	glueRoleARN, glueRoleName, gluePolicyARN, err := createGlueRole(ctx, iamClient, resourceName, bucket, prefix)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Glue IAM role", err.Error())
		return
	}
	state.GlueRoleARN = glueRoleARN
	state.GlueRoleName = glueRoleName
	state.GluePolicyARN = gluePolicyARN

	lambdaRoleARN, lambdaRoleName, lambdaPolicyARN, err := createLambdaRole(ctx, iamClient, resourceName, bucket, prefix)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Lambda IAM role", err.Error())
		return
	}
	state.LambdaRoleARN = lambdaRoleARN
	state.LambdaRoleName = lambdaRoleName
	state.LambdaPolicyARN = lambdaPolicyARN

	// Step 2: Upload Glue script to source bucket.
	if err := uploadGlueScript(ctx, s3Client, scriptBucket, scriptKey); err != nil {
		resp.Diagnostics.AddError("Failed to upload Glue script", err.Error())
		return
	}

	// Step 3: Create and run Glue job.
	glueJobName := fmt.Sprintf("rawtree-ingest-%s", resourceName)
	state.GlueJobName = glueJobName

	ingestEndpoint := r.client.TableURL("", table, nil) // the scripts add database= next to url=

	glueParams := map[string]string{
		"BUCKET":          bucket,
		"PREFIX":          prefix,
		"FILE_PATTERN":    filePattern,
		"FORMAT":          format,
		"API_URL":         r.client.APIURL,
		"API_KEY":         r.client.APIKey,
		"TABLE":           table,
		"INGEST_ENDPOINT": ingestEndpoint,
		"DATABASE":        database,
		"CONCURRENCY":     "10",
	}

	if err := createGlueJob(ctx, glueClient, glueJobName, glueRoleARN, scriptBucket, scriptKey, glueParams); err != nil {
		resp.Diagnostics.AddError("Failed to create Glue job", err.Error())
		return
	}

	// Wait for IAM role propagation before starting the job.
	// Glue needs the role to download the script from S3.
	time.Sleep(15 * time.Second)

	runID, err := startGlueJobRun(ctx, glueClient, glueJobName)
	if err != nil {
		resp.Diagnostics.AddError("Failed to start Glue job run", err.Error())
		return
	}

	tflog.Info(ctx, "Glue job run started", map[string]interface{}{
		"job_name": glueJobName,
		"run_id":   runID,
	})

	// Step 4: Create Lambda function.
	lambdaFunctionName := fmt.Sprintf("rawtree-ingest-%s", resourceName)
	state.LambdaFunctionName = lambdaFunctionName

	lambdaEnvVars := map[string]string{
		"API_URL":         r.client.APIURL,
		"API_KEY":         r.client.APIKey,
		"TABLE":           table,
		"FORMAT":          format,
		"FILE_PATTERN":    filePattern,
		"PREFIX":          prefix,
		"INGEST_ENDPOINT": ingestEndpoint,
		"DATABASE":        database,
	}

	lambdaARN, err := createLambdaFunction(ctx, lambdaClient, lambdaFunctionName, lambdaRoleARN, lambdaEnvVars)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Lambda function", err.Error())
		return
	}

	// Step 5: Enable EventBridge on the S3 bucket.
	if err := enableS3EventBridge(ctx, s3Client, bucket); err != nil {
		resp.Diagnostics.AddError("Failed to enable EventBridge on S3 bucket", err.Error())
		return
	}

	// Step 6: Create EventBridge rule and target.
	ebRuleName := fmt.Sprintf("rawtree-s3-%s", resourceName)
	state.EventBridgeRuleName = ebRuleName

	ruleARN, err := createEventBridgeRule(ctx, ebClient, ebRuleName, bucket, prefix)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create EventBridge rule", err.Error())
		return
	}

	targetID := fmt.Sprintf("rawtree-lambda-%s", resourceName)
	state.EventBridgeTargetID = targetID

	if err := addEventBridgeTarget(ctx, ebClient, ebRuleName, targetID, lambdaARN); err != nil {
		resp.Diagnostics.AddError("Failed to add EventBridge target", err.Error())
		return
	}

	// Step 7: Add Lambda permission for EventBridge.
	if err := addLambdaPermission(ctx, lambdaClient, lambdaFunctionName, ruleARN); err != nil {
		resp.Diagnostics.AddError("Failed to add Lambda permission", err.Error())
		return
	}

	// Set state.
	plan.ID = types.StringValue(resourceName)
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Database = types.StringValue(database)
	plan.GlueJobName = types.StringValue(glueJobName)
	plan.GlueJobRunID = types.StringValue(runID)
	plan.LambdaFunctionARN = types.StringValue(lambdaARN)
	plan.EventBridgeRuleARN = types.StringValue(ruleARN)

	// Store internal AWS resource state in private state.
	stateJSON, _ := json.Marshal(state)
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *S3IngestionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data S3IngestionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read AWS resource state from private state.
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

	// Verify Glue job exists.
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	glueClient := glue.NewFromConfig(awsCfg)
	_, err = glueClient.GetJob(ctx, &glue.GetJobInput{
		JobName: &state.GlueJobName,
	})
	if err != nil {
		// Resource no longer exists.
		resp.State.RemoveResource(ctx)
		return
	}

	// Refresh provider-derived values so changes trigger an update.
	data.APIURL = types.StringValue(r.client.APIURL)
	data.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))

	// Preserve private state.
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *S3IngestionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan S3IngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read AWS resource state.
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

	// Only table and file_pattern can be updated in place.
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	lambdaClient := lambda.NewFromConfig(awsCfg)

	database := r.client.DatabaseOr(plan.Database.ValueString())

	ingestEndpoint := r.client.TableURL("", plan.Table.ValueString(), nil)

	// Update Lambda environment variables.
	envVars := map[string]string{
		"API_URL":         r.client.APIURL,
		"API_KEY":         r.client.APIKey,
		"TABLE":           plan.Table.ValueString(),
		"FORMAT":          plan.Format.ValueString(),
		"FILE_PATTERN":    plan.FilePattern.ValueString(),
		"PREFIX":          plan.Prefix.ValueString(),
		"INGEST_ENDPOINT": ingestEndpoint,
		"DATABASE":        database,
	}

	if err := updateLambdaCode(ctx, lambdaClient, state.LambdaFunctionName); err != nil {
		resp.Diagnostics.AddError("Failed to update Lambda function code", err.Error())
		return
	}

	if err := updateLambdaEnvVars(ctx, lambdaClient, state.LambdaFunctionName, envVars); err != nil {
		resp.Diagnostics.AddError("Failed to update Lambda function", err.Error())
		return
	}

	// Update provider-derived values in state.
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Database = types.StringValue(database)

	// Preserve private state.
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *S3IngestionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data S3IngestionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read AWS resource state.
	stateJSON, diags := req.Private.GetKey(ctx, "aws_resources")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if stateJSON == nil {
		resp.Diagnostics.AddError(
			"Missing Internal State",
			"Cannot destroy because private AWS resource state is missing. "+
				"AWS resources (Glue job, Lambda, EventBridge rule, S3 notification, IAM roles, etc.) may still exist. "+
				"Remove them manually, then use `terraform state rm` to drop this resource from state.",
		)
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
	glueClient := glue.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg)
	lambdaClient := lambda.NewFromConfig(awsCfg)
	ebClient := eventbridge.NewFromConfig(awsCfg)

	tflog.Info(ctx, "Deleting S3 ingestion resource", map[string]interface{}{
		"glue_job": state.GlueJobName,
		"lambda":   state.LambdaFunctionName,
	})

	// Delete in reverse order of creation.

	// EventBridge rule and target.
	if err := deleteEventBridgeRule(ctx, ebClient, state.EventBridgeRuleName, state.EventBridgeTargetID); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete EventBridge rule", err.Error())
	}

	// Lambda function.
	if err := deleteLambdaFunction(ctx, lambdaClient, state.LambdaFunctionName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Lambda function", err.Error())
	}

	// Glue job.
	if err := deleteGlueJob(ctx, glueClient, state.GlueJobName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Glue job", err.Error())
	}

	// Glue script object (stored in source bucket).
	if err := deleteGlueScriptObject(ctx, s3Client, state.ScriptBucket, state.ScriptKey); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Glue script", err.Error())
	}

	// IAM roles.
	if err := util.DeleteRole(ctx, iamClient, state.GlueRoleName, state.GluePolicyARN, "arn:aws:iam::aws:policy/service-role/AWSGlueServiceRole"); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Glue IAM role", err.Error())
	}

	if err := util.DeleteRole(ctx, iamClient, state.LambdaRoleName, state.LambdaPolicyARN, "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Lambda IAM role", err.Error())
	}
}

func (r *S3IngestionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"The rawtree_s3_ingestion resource does not support import. "+
			"Please create the resource using Terraform.",
	)
}
