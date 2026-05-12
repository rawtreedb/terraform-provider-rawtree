package mongo_connector

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// MongoConnectorModel describes the resource data model.
type MongoConnectorModel struct {
	ID types.String `tfsdk:"id"`

	// User-specified attributes.
	Table         types.String `tfsdk:"table"`
	MongoURI      types.String `tfsdk:"mongo_uri"`
	MongoDatabase types.String `tfsdk:"mongo_database"`
	Region        types.String `tfsdk:"region"`

	// Optional configuration.
	Collections    types.String `tfsdk:"collections"`
	TablePrefix    types.String `tfsdk:"table_prefix"`
	FullDocument   types.String `tfsdk:"full_document"`
	SnapshotEnable types.Bool   `tfsdk:"snapshot_enabled"`
	BatchMaxRows   types.Int64  `tfsdk:"batch_max_rows"`
	FlushInterval  types.String `tfsdk:"flush_interval"`
	ImageTag       types.String `tfsdk:"image_tag"`

	// Provider-derived.
	APIURL       types.String `tfsdk:"api_url"`
	APIKeyHash   types.String `tfsdk:"api_key_hash"`
	Organization types.String `tfsdk:"organization"`
	Project      types.String `tfsdk:"project"`

	// Computed.
	ECSClusterARN  types.String `tfsdk:"ecs_cluster_arn"`
	ECSServiceARN  types.String `tfsdk:"ecs_service_arn"`
	TaskDefinition types.String `tfsdk:"task_definition_arn"`
	LogGroupName   types.String `tfsdk:"log_group_name"`
	SecretARN      types.String `tfsdk:"secret_arn"`
}

// awsResourceState tracks all AWS resources created by this resource for cleanup.
type awsResourceState struct {
	Region             string `json:"region"`
	ECSClusterARN      string `json:"ecs_cluster_arn"`
	ECSClusterName     string `json:"ecs_cluster_name"`
	ECSServiceName     string `json:"ecs_service_name"`
	TaskDefinitionARN  string `json:"task_definition_arn"`
	TaskFamily         string `json:"task_family"`
	LogGroupName       string `json:"log_group_name"`
	SecretARN          string `json:"secret_arn"`
	SecretName         string `json:"secret_name"`
	IAMRoleName        string `json:"iam_role_name"`
	IAMRoleARN         string `json:"iam_role_arn"`
	IAMPolicyARN       string `json:"iam_policy_arn"`
	ExecutionRoleName  string `json:"execution_role_name"`
	ExecutionRoleARN   string `json:"execution_role_arn"`
	ExecutionPolicyARN string `json:"execution_policy_arn"`
}
