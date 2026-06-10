package cloudfront_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type CloudfrontIngestionModel struct {
	ID types.String `tfsdk:"id"`

	// User-specified attributes.
	Table          types.String `tfsdk:"table"`
	DistributionID types.String `tfsdk:"distribution_id"`
	Region         types.String `tfsdk:"region"`

	// Optional tuning.
	SamplingRate      types.Int64  `tfsdk:"sampling_rate"`
	Fields            types.List   `tfsdk:"fields"`
	BufferingSize     types.Int64  `tfsdk:"buffering_size"`
	BufferingInterval types.Int64  `tfsdk:"buffering_interval"`
	S3BackupMode      types.String `tfsdk:"s3_backup_mode"`

	// Provider-derived attributes (trigger update when provider config changes).
	APIURL       types.String `tfsdk:"api_url"`
	APIKeyHash   types.String `tfsdk:"api_key_hash"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`

	// Full Firehose HTTP endpoint URL.
	EndpointURL types.String `tfsdk:"endpoint_url"`

	// Computed attributes.
	KinesisStreamARN     types.String `tfsdk:"kinesis_stream_arn"`
	KinesisStreamName    types.String `tfsdk:"kinesis_stream_name"`
	FirehoseARN          types.String `tfsdk:"firehose_arn"`
	FirehoseName         types.String `tfsdk:"firehose_name"`
	BackupBucketName     types.String `tfsdk:"backup_bucket_name"`
	RealtimeLogConfigARN types.String `tfsdk:"realtime_log_config_arn"`
}

type awsResourceState struct {
	KinesisStreamName     string `json:"kinesis_stream_name"`
	KinesisStreamARN      string `json:"kinesis_stream_arn"`
	FirehoseName          string `json:"firehose_name"`
	FirehoseARN           string `json:"firehose_arn"`
	BackupBucketName      string `json:"backup_bucket_name"`
	CloudFrontRoleName    string `json:"cloudfront_role_name"`
	CloudFrontRoleARN     string `json:"cloudfront_role_arn"`
	CloudFrontPolicyARN   string `json:"cloudfront_policy_arn"`
	FirehoseRoleName      string `json:"firehose_role_name"`
	FirehoseRoleARN       string `json:"firehose_role_arn"`
	FirehosePolicyARN     string `json:"firehose_policy_arn"`
	RealtimeLogConfigARN  string `json:"realtime_log_config_arn"`
	RealtimeLogConfigName string `json:"realtime_log_config_name"`
	DistributionID        string `json:"distribution_id"`
	Region                string `json:"region"`
}
