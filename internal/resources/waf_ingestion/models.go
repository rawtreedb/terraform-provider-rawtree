package waf_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// WafIngestionModel describes the resource data model.
type WafIngestionModel struct {
	ID types.String `tfsdk:"id"`

	// User-specified attributes.
	Table     types.String `tfsdk:"table"`
	WebACLARN types.String `tfsdk:"web_acl_arn"`
	Region    types.String `tfsdk:"region"`

	// Optional tuning.
	BufferingSize     types.Int64  `tfsdk:"buffering_size"`
	BufferingInterval types.Int64  `tfsdk:"buffering_interval"`
	S3BackupMode      types.String `tfsdk:"s3_backup_mode"`

	// Provider-derived attributes (trigger update when provider config changes).
	APIURL       types.String `tfsdk:"api_url"`
	APIKeyHash   types.String `tfsdk:"api_key_hash"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`

	// Computed attributes.
	FirehoseARN               types.String `tfsdk:"firehose_arn"`
	FirehoseName              types.String `tfsdk:"firehose_name"`
	BackupBucketName          types.String `tfsdk:"backup_bucket_name"`
	WafLoggingConfigurationID types.String `tfsdk:"waf_logging_configuration_id"`
}

// awsResourceState tracks all AWS resources created by this resource for cleanup.
type awsResourceState struct {
	FirehoseName     string `json:"firehose_name"`
	FirehoseARN      string `json:"firehose_arn"`
	BackupBucketName string `json:"backup_bucket_name"`
	IAMRoleName      string `json:"iam_role_name"`
	IAMRoleARN       string `json:"iam_role_arn"`
	IAMPolicyARN     string `json:"iam_policy_arn"`
	WebACLARN        string `json:"web_acl_arn"`
	Region           string `json:"region"`
}
