package waf_ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/firehose"
	fhtypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/client"
	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

var (
	_ resource.Resource                = &WafIngestionResource{}
	_ resource.ResourceWithImportState = &WafIngestionResource{}
)

type WafIngestionResource struct {
	client *client.RawtreeClient
}

func NewResource() resource.Resource {
	return &WafIngestionResource{}
}

func (r *WafIngestionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_waf_ingestion"
}

func (r *WafIngestionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema()
}

func (r *WafIngestionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WafIngestionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WafIngestionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := plan.Region.ValueString()
	table := plan.Table.ValueString()
	webACLARN := plan.WebACLARN.ValueString()
	bufferingSizeMB := int32(plan.BufferingSize.ValueInt64())
	bufferingSeconds := int32(plan.BufferingInterval.ValueInt64())
	s3BackupMode := plan.S3BackupMode.ValueString()

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
	wafClient := wafv2.NewFromConfig(awsCfg)

	state := awsResourceState{Region: region, WebACLARN: webACLARN}

	// Step 1: Create S3 backup bucket.
	bucketName := fmt.Sprintf("rawtree-waf-backup-%s", resourceName)
	if err := createBackupBucket(ctx, s3Client, bucketName, region); err != nil {
		resp.Diagnostics.AddError("Failed to create S3 backup bucket", err.Error())
		return
	}
	state.BackupBucketName = bucketName

	// Step 2: Create IAM role for Firehose.
	roleARN, roleName, policyARN, err := createFirehoseRole(ctx, iamClient, resourceName, bucketName, region)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Firehose IAM role", err.Error())
		return
	}
	state.IAMRoleARN = roleARN
	state.IAMRoleName = roleName
	state.IAMPolicyARN = policyARN

	// Step 3: Create Firehose delivery stream.
	firehoseName := fmt.Sprintf("aws-waf-logs-rawtree-%s", resourceName)
	endpointURL := fmt.Sprintf("%s/v1/%s/%s/tables/%s?transform=firehose",
		r.client.APIURL, org, project, table)

	cfg := firehoseConfig{
		Name:             firehoseName,
		EndpointURL:      endpointURL,
		AccessKey:        r.client.APIKey,
		RoleARN:          roleARN,
		BucketARN:        fmt.Sprintf("arn:aws:s3:::%s", bucketName),
		BufferingSizeMB:  bufferingSizeMB,
		BufferingSeconds: bufferingSeconds,
		S3BackupMode:     s3BackupMode,
	}

	firehoseARN, err := createDeliveryStream(ctx, firehoseClient, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Firehose delivery stream", err.Error())
		return
	}
	state.FirehoseName = firehoseName
	state.FirehoseARN = firehoseARN

	tflog.Info(ctx, "Waiting for Firehose to become ACTIVE", map[string]interface{}{
		"firehose_name": firehoseName,
	})

	if err := waitForFirehoseActive(ctx, firehoseClient, firehoseName, 3*time.Minute); err != nil {
		resp.Diagnostics.AddError("Firehose did not become active", err.Error())
		return
	}

	// Step 4: Put WAF logging configuration.
	if err := putLoggingConfiguration(ctx, wafClient, webACLARN, firehoseARN); err != nil {
		resp.Diagnostics.AddError("Failed to put WAF logging configuration", err.Error())
		return
	}

	// Set state.
	plan.ID = types.StringValue(resourceName)
	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)
	plan.FirehoseARN = types.StringValue(firehoseARN)
	plan.FirehoseName = types.StringValue(firehoseName)
	plan.BackupBucketName = types.StringValue(bucketName)
	plan.WafLoggingConfigurationID = types.StringValue(webACLARN)

	stateJSON, _ := json.Marshal(state)
	resp.Private.SetKey(ctx, "aws_resources", stateJSON)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WafIngestionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data WafIngestionModel
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

	// Verify Firehose exists.
	_, err = firehoseClient.DescribeDeliveryStream(ctx, &firehose.DescribeDeliveryStreamInput{
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

	// Refresh provider-derived values.
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

func (r *WafIngestionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WafIngestionModel
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
	endpointURL := fmt.Sprintf("%s/v1/%s/%s/tables/%s?transform=firehose",
		r.client.APIURL, org, project, table)

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
		RoleARN:          state.IAMRoleARN,
		BucketARN:        fmt.Sprintf("arn:aws:s3:::%s", state.BackupBucketName),
		BufferingSizeMB:  int32(plan.BufferingSize.ValueInt64()),
		BufferingSeconds: int32(plan.BufferingInterval.ValueInt64()),
		S3BackupMode:     plan.S3BackupMode.ValueString(),
	}

	if err := updateDeliveryStream(ctx, firehoseClient, state.FirehoseName, cfg); err != nil {
		resp.Diagnostics.AddError("Failed to update Firehose delivery stream", err.Error())
		return
	}

	plan.APIURL = types.StringValue(r.client.APIURL)
	plan.APIKeyHash = types.StringValue(util.HashString(r.client.APIKey))
	plan.Organization = types.StringValue(org)
	plan.Project = types.StringValue(project)

	resp.Private.SetKey(ctx, "aws_resources", stateJSON)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WafIngestionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WafIngestionModel
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
	wafClient := wafv2.NewFromConfig(awsCfg)

	tflog.Info(ctx, "Deleting WAF ingestion resource", map[string]interface{}{
		"firehose": state.FirehoseName,
		"web_acl":  state.WebACLARN,
	})

	// Delete in reverse order.

	// 1. Delete WAF logging configuration.
	if err := deleteLoggingConfiguration(ctx, wafClient, state.WebACLARN); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete WAF logging configuration", err.Error())
	}

	// 2. Delete Firehose delivery stream.
	if err := deleteDeliveryStream(ctx, firehoseClient, state.FirehoseName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Firehose delivery stream", err.Error())
	} else {
		if err := waitForFirehoseDeleted(ctx, firehoseClient, state.FirehoseName, 5*time.Minute); err != nil {
			resp.Diagnostics.AddWarning("Firehose deletion timeout", err.Error())
		}
	}

	// 3. Delete IAM role.
	if err := util.DeleteRole(ctx, iamClient, state.IAMRoleName, state.IAMPolicyARN, ""); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete Firehose IAM role", err.Error())
	}

	// 4. Delete S3 backup bucket.
	if err := deleteBackupBucket(ctx, s3Client, state.BackupBucketName); err != nil {
		resp.Diagnostics.AddWarning("Failed to delete S3 backup bucket", err.Error())
	}
}

func (r *WafIngestionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"The rawtree_waf_ingestion resource does not support import. "+
			"Please create the resource using Terraform.",
	)
}
