package cloudfront_ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var (
	_ resource.Resource                = &CloudfrontIngestionResource{}
	_ resource.ResourceWithImportState = &CloudfrontIngestionResource{}
	_ resource.ResourceWithModifyPlan  = &CloudfrontIngestionResource{}
)

type CloudfrontIngestionResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &CloudfrontIngestionResource{}
}

func (r *CloudfrontIngestionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cloudfront_ingestion"
}

func (r *CloudfrontIngestionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *CloudfrontIngestionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if r.client == nil {
		return
	}

	var plan CloudfrontIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
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
	table := plan.Table.ValueString()

	fields := extractFieldsFromList(ctx, plan.Fields)

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.EndpointURL = types.StringValue(buildEndpointURL(r.client.APIURL, org, project, table, fields))
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *CloudfrontIngestionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CloudfrontIngestionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CloudfrontIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := plan.Region.ValueString()
	table := plan.Table.ValueString()
	distributionID := plan.DistributionID.ValueString()
	samplingRate := plan.SamplingRate.ValueInt64()
	bufferingSizeMB := int32(plan.BufferingSize.ValueInt64())
	bufferingSeconds := int32(plan.BufferingInterval.ValueInt64())
	s3BackupMode := plan.S3BackupMode.ValueString()

	var fields []string
	resp.Diagnostics.Append(plan.Fields.ElementsAs(ctx, &fields, false)...)
	if resp.Diagnostics.HasError() {
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

	resourceName := util.SanitizeResourceName(fmt.Sprintf("%s-%s-%s", org, project, table))

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to load AWS config: %s", err))
		return
	}

	iamClient := iam.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg)
	firehoseClient := firehose.NewFromConfig(awsCfg)
	kinesisClient := kinesis.NewFromConfig(awsCfg)
	cfClient := cloudfront.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)

	state := awsResourceState{Region: region, DistributionID: distributionID}

	// Step 1: Create S3 backup bucket.
	bucketName := fmt.Sprintf("rawtree-cf-backup-%s", resourceName)
	if err := util.CreateBackupBucket(ctx, s3Client, bucketName, region, "cloudfront-firehose-backup"); err != nil {
		resp.Diagnostics.AddError("Failed to create S3 backup bucket", err.Error())
		return
	}
	state.BackupBucketName = bucketName

	// Step 2: Create Kinesis Data Stream.
	kinesisStreamName := fmt.Sprintf("rawtree-cf-%s", resourceName)
	if err := createKinesisStream(ctx, kinesisClient, kinesisStreamName); err != nil {
		resp.Diagnostics.AddError("Failed to create Kinesis Data Stream", err.Error())
		return
	}
	state.KinesisStreamName = kinesisStreamName

	tflog.Info(ctx, "Waiting for Kinesis Data Stream to become ACTIVE", map[string]interface{}{
		"stream_name": kinesisStreamName,
	})

	// Step 3: Wait for Kinesis ACTIVE and get ARN.
	kinesisStreamARN, err := waitForKinesisActive(ctx, kinesisClient, kinesisStreamName, 3*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("Kinesis Data Stream did not become active", err.Error())
		return
	}
	state.KinesisStreamARN = kinesisStreamARN

	// Step 4: Create IAM role for CloudFront -> Kinesis.
	cfRoleARN, cfRoleName, cfPolicyARN, err := createCloudFrontRole(ctx, iamClient, resourceName, kinesisStreamARN)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CloudFront IAM role", err.Error())
		return
	}
	state.CloudFrontRoleARN = cfRoleARN
	state.CloudFrontRoleName = cfRoleName
	state.CloudFrontPolicyARN = cfPolicyARN

	// Step 5: Create IAM role for Firehose -> Kinesis + S3 + CloudWatch.
	fhRoleARN, fhRoleName, fhPolicyARN, err := createFirehoseRole(ctx, iamClient, resourceName, kinesisStreamARN, bucketName, region)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Firehose IAM role", err.Error())
		return
	}
	state.FirehoseRoleARN = fhRoleARN
	state.FirehoseRoleName = fhRoleName
	state.FirehosePolicyARN = fhPolicyARN

	// Step 6 (IAM propagation wait is handled inside createFirehoseRole).

	// Step 7: Create Firehose delivery stream.
	firehoseName := fmt.Sprintf("rawtree-cf-%s", resourceName)
	endpointURL := buildEndpointURL(r.client.APIURL, org, project, table, fields)

	cfg := firehoseConfig{
		Name:             firehoseName,
		EndpointURL:      endpointURL,
		AccessKey:        r.client.APIKey,
		FirehoseRoleARN:  fhRoleARN,
		KinesisStreamARN: kinesisStreamARN,
		BucketARN:        fmt.Sprintf("arn:aws:s3:::%s", bucketName),
		BufferingSizeMB:  bufferingSizeMB,
		BufferingSeconds: bufferingSeconds,
		S3BackupMode:     s3BackupMode,
		Region:           region,
	}

	firehoseARN, err := createDeliveryStream(ctx, firehoseClient, logsClient, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Firehose delivery stream", err.Error())
		return
	}
	state.FirehoseName = firehoseName
	state.FirehoseARN = firehoseARN

	tflog.Info(ctx, "Waiting for Firehose to become ACTIVE", map[string]interface{}{
		"firehose_name": firehoseName,
	})

	// Step 8: Wait for Firehose ACTIVE.
	if err := waitForFirehoseActive(ctx, firehoseClient, firehoseName, 3*time.Minute); err != nil {
		resp.Diagnostics.AddError("Firehose did not become active", err.Error())
		return
	}

	// Step 9: Create CloudFront real-time log config.
	logConfigName := fmt.Sprintf("rawtree-cf-%s", resourceName)
	logConfigARN, err := createRealtimeLogConfig(ctx, cfClient, logConfigName, fields, samplingRate, kinesisStreamARN, cfRoleARN)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CloudFront real-time log config", err.Error())
		return
	}
	state.RealtimeLogConfigARN = logConfigARN
	state.RealtimeLogConfigName = logConfigName

	// Step 10: Attach to distribution.
	if err := attachToDistribution(ctx, cfClient, distributionID, logConfigARN); err != nil {
		resp.Diagnostics.AddError("Failed to attach real-time log config to distribution", err.Error())
		return
	}

	// Set state.
	plan.ID = types.StringValue(resourceName)
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.EndpointURL = types.StringValue(endpointURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)
	plan.KinesisStreamARN = types.StringValue(kinesisStreamARN)
	plan.KinesisStreamName = types.StringValue(kinesisStreamName)
	plan.FirehoseARN = types.StringValue(firehoseARN)
	plan.FirehoseName = types.StringValue(firehoseName)
	plan.BackupBucketName = types.StringValue(bucketName)
	plan.RealtimeLogConfigARN = types.StringValue(logConfigARN)

	stateJSON, _ := json.Marshal(state)
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CloudfrontIngestionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data CloudfrontIngestionModel
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

	firehoseClient := firehose.NewFromConfig(awsCfg)

	descOut, err := firehoseClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
		DeliveryStreamName: &state.FirehoseName,
	})
	if err != nil {
		var notFound *fhtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Firehose delivery stream",
			fmt.Sprintf("Error describing Firehose %s: %s", state.FirehoseName, err))
		return
	}

	actualEndpointURL := ""
	actualAPIURL := r.client.APIURL
	if dests := descOut.DeliveryStreamDescription.Destinations; len(dests) > 0 {
		if httpDest := dests[0].HttpEndpointDestinationDescription; httpDest != nil {
			if ep := httpDest.EndpointConfiguration; ep != nil && ep.Url != nil {
				actualEndpointURL = *ep.Url
				actualAPIURL = util.ExtractBaseURL(*ep.Url)
			}
		}
	}

	data.APIURL = types.StringValue(actualAPIURL)
	data.EndpointURL = types.StringValue(actualEndpointURL)
	if data.Organization.IsNull() || data.Organization.ValueString() == "" {
		data.Organization = types.StringValue(r.client.Organization)
	}
	if data.Project.IsNull() || data.Project.ValueString() == "" {
		data.Project = types.StringValue(r.client.Project)
	}

	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CloudfrontIngestionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CloudfrontIngestionModel
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

	table := plan.Table.ValueString()
	fields := extractFieldsFromList(ctx, plan.Fields)
	endpointURL := buildEndpointURL(r.client.APIURL, org, project, table, fields)

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(state.Region))
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", err.Error())
		return
	}

	firehoseClient := firehose.NewFromConfig(awsCfg)

	cfg := firehoseConfig{
		Name:             state.FirehoseName,
		EndpointURL:      endpointURL,
		AccessKey:        r.client.APIKey,
		FirehoseRoleARN:  state.FirehoseRoleARN,
		KinesisStreamARN: state.KinesisStreamARN,
		BucketARN:        fmt.Sprintf("arn:aws:s3:::%s", state.BackupBucketName),
		BufferingSizeMB:  int32(plan.BufferingSize.ValueInt64()),
		BufferingSeconds: int32(plan.BufferingInterval.ValueInt64()),
		S3BackupMode:     plan.S3BackupMode.ValueString(),
	}

	if err := updateDeliveryStream(ctx, firehoseClient, state.FirehoseName, cfg); err != nil {
		resp.Diagnostics.AddError("Failed to update Firehose delivery stream", err.Error())
		return
	}

	// Update real-time log config if sampling rate or fields changed.
	cfClient := cloudfront.NewFromConfig(awsCfg)
	if err := updateRealtimeLogConfig(ctx, cfClient, state.RealtimeLogConfigARN, fields, plan.SamplingRate.ValueInt64(), state.KinesisStreamARN, state.CloudFrontRoleARN); err != nil {
		resp.Diagnostics.AddError("Failed to update real-time log config", err.Error())
		return
	}

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.EndpointURL = types.StringValue(endpointURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)

	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CloudfrontIngestionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CloudfrontIngestionModel
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
	s3Client := s3.NewFromConfig(awsCfg)
	firehoseClient := firehose.NewFromConfig(awsCfg)
	kinesisClient := kinesis.NewFromConfig(awsCfg)
	cfClient := cloudfront.NewFromConfig(awsCfg)
	logsClient := cloudwatchlogs.NewFromConfig(awsCfg)

	tflog.Info(ctx, "Deleting CloudFront ingestion resource", map[string]interface{}{
		"firehose":        state.FirehoseName,
		"kinesis_stream":  state.KinesisStreamName,
		"distribution_id": state.DistributionID,
	})

	// 1. Detach from distribution.
	if err := detachFromDistribution(ctx, cfClient, state.DistributionID); err != nil {
		resp.Diagnostics.AddWarning("Failed to detach real-time log config from distribution", err.Error())
	}

	// 2. Delete real-time log config (retry in case distribution detach is still propagating).
	if state.RealtimeLogConfigARN != "" {
		if err := deleteRealtimeLogConfigWithRetry(ctx, cfClient, state.RealtimeLogConfigARN, 3*time.Minute); err != nil {
			resp.Diagnostics.AddWarning("Failed to delete CloudFront real-time log config", err.Error())
		}
	}

	// 3. Delete Firehose delivery stream.
	if err := deleteDeliveryStream(ctx, firehoseClient, state.FirehoseName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Firehose delivery stream", err.Error())
	} else {
		if err := waitForFirehoseDeleted(ctx, firehoseClient, state.FirehoseName, 5*time.Minute); err != nil {
			resp.Diagnostics.AddWarning("Firehose deletion timeout", err.Error())
		}
	}

	// 4. Delete Kinesis Data Stream.
	if err := deleteKinesisStream(ctx, kinesisClient, state.KinesisStreamName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Kinesis Data Stream", err.Error())
	}

	// 5. Delete IAM roles.
	if err := util.DeleteRole(ctx, iamClient, state.FirehoseRoleName, state.FirehosePolicyARN, ""); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Firehose IAM role", err.Error())
	}
	if err := util.DeleteRole(ctx, iamClient, state.CloudFrontRoleName, state.CloudFrontPolicyARN, ""); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete CloudFront IAM role", err.Error())
	}

	// 6. Delete S3 backup bucket.
	if err := util.DeleteBackupBucket(ctx, s3Client, state.BackupBucketName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete S3 backup bucket", err.Error())
	}

	// 7. Delete CloudWatch log group created during Firehose setup.
	logGroup := fmt.Sprintf("/aws/firehose/%s", state.FirehoseName)
	if _, err := logsClient.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: &logGroup,
	}); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete CloudWatch log group", err.Error())
	}
}

func (r *CloudfrontIngestionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"The rawtree_cloudfront_ingestion resource does not support import. "+
			"Please create the resource using Terraform.",
	)
}

func extractFieldsFromList(ctx context.Context, fieldList types.List) []string {
	if fieldList.IsNull() || fieldList.IsUnknown() {
		return defaultFields
	}
	var elems []types.String
	fieldList.ElementsAs(ctx, &elems, false)
	result := make([]string, 0, len(elems))
	for _, e := range elems {
		if !e.IsNull() && !e.IsUnknown() {
			result = append(result, e.ValueString())
		}
	}
	if len(result) == 0 {
		return defaultFields
	}
	return result
}

func buildEndpointURL(apiURL, org, project, table string, fields []string) string {
	return fmt.Sprintf("%s/v1/%s/%s/tables/%s?transform=firehose&columns=%s",
		apiURL, org, project, table, strings.Join(sortFieldsCanonical(fields), ","))
}
