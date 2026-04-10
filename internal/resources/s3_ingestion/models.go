package s3_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// S3IngestionModel describes the resource data model.
type S3IngestionModel struct {
	ID types.String `tfsdk:"id"`

	// User-specified attributes.
	Table       types.String `tfsdk:"table"`
	Bucket      types.String `tfsdk:"bucket"`
	Prefix      types.String `tfsdk:"prefix"`
	FilePattern types.String `tfsdk:"file_pattern"`
	Format      types.String `tfsdk:"format"`
	Region      types.String `tfsdk:"region"`

	// Computed attributes.
	GlueJobName        types.String `tfsdk:"glue_job_name"`
	GlueJobRunID       types.String `tfsdk:"glue_job_run_id"`
	LambdaFunctionARN  types.String `tfsdk:"lambda_function_arn"`
	EventBridgeRuleARN types.String `tfsdk:"eventbridge_rule_arn"`

	// Internal state for cleanup (not exposed in schema, stored in private state).
	GlueRoleARN       string `tfsdk:"-"`
	LambdaRoleARN     string `tfsdk:"-"`
	ScriptBucketName  string `tfsdk:"-"`
	EventBridgeTarget string `tfsdk:"-"`
}

// awsResourceState tracks all AWS resources created by this resource for cleanup.
type awsResourceState struct {
	GlueJobName         string `json:"glue_job_name"`
	GlueRoleARN         string `json:"glue_role_arn"`
	GlueRoleName        string `json:"glue_role_name"`
	GluePolicyARN       string `json:"glue_policy_arn"`
	LambdaFunctionName  string `json:"lambda_function_name"`
	LambdaRoleARN       string `json:"lambda_role_arn"`
	LambdaRoleName      string `json:"lambda_role_name"`
	LambdaPolicyARN     string `json:"lambda_policy_arn"`
	ScriptBucket        string `json:"script_bucket"`
	ScriptKey           string `json:"script_key"`
	EventBridgeRuleName string `json:"eventbridge_rule_name"`
	EventBridgeTargetID string `json:"eventbridge_target_id"`
	Region              string `json:"region"`
}
