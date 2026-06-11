package supabase_cdc_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// SupabaseCDCIngestionModel describes the resource data model.
type SupabaseCDCIngestionModel struct {
	ID types.String `tfsdk:"id"`

	// User-specified attributes.
	Name                  types.String `tfsdk:"name"`
	Region                types.String `tfsdk:"region"`
	Publication           types.String `tfsdk:"publication"`
	PipelineID            types.String `tfsdk:"pipeline_id"`
	Image                 types.String `tfsdk:"image"`
	CPU                   types.Int64  `tfsdk:"cpu"`
	Memory                types.Int64  `tfsdk:"memory"`
	SubnetIDs             types.List   `tfsdk:"subnet_ids"`
	SecurityGroupIDs      types.List   `tfsdk:"security_group_ids"`
	AssignPublicIP        types.Bool   `tfsdk:"assign_public_ip"`
	DatabaseURL           types.String `tfsdk:"database_url"`
	DatabaseURLSecretARN  types.String `tfsdk:"database_url_secret_arn"`
	TLSRootCertPEM        types.String `tfsdk:"tls_root_cert_pem"`
	TLSRootCertSecretARN  types.String `tfsdk:"tls_root_cert_secret_arn"`
	LogRetentionDays      types.Int64  `tfsdk:"log_retention_days"`
	RunInitializationTask types.Bool   `tfsdk:"run_initialization_task"`
	InitializationCommand types.List   `tfsdk:"initialization_command"`
	WorkerCommand         types.List   `tfsdk:"worker_command"`
	Environment           types.Map    `tfsdk:"environment"`
	Organization          types.String `tfsdk:"organization"`
	Project               types.String `tfsdk:"project"`

	// Provider-derived attributes.
	APIURL     types.String `tfsdk:"api_url"`
	APIKeyHash types.String `tfsdk:"api_key_hash"`

	// Computed attributes.
	ClusterARN        types.String `tfsdk:"cluster_arn"`
	ServiceARN        types.String `tfsdk:"service_arn"`
	TaskDefinitionARN types.String `tfsdk:"task_definition_arn"`
	LogGroupName      types.String `tfsdk:"log_group_name"`
	ExecutionRoleARN  types.String `tfsdk:"execution_role_arn"`
	ConfigSecretARN   types.String `tfsdk:"config_secret_arn"`
}

// awsResourceState tracks all AWS resources created by this resource for cleanup.
type awsResourceState struct {
	Region               string `json:"region"`
	ResourceName         string `json:"resource_name"`
	ClusterARN           string `json:"cluster_arn"`
	ClusterName          string `json:"cluster_name"`
	ServiceARN           string `json:"service_arn"`
	ServiceName          string `json:"service_name"`
	TaskDefinitionARN    string `json:"task_definition_arn"`
	TaskDefinitionFamily string `json:"task_definition_family"`
	ExecutionRoleARN     string `json:"execution_role_arn"`
	ExecutionRoleName    string `json:"execution_role_name"`
	ExecutionPolicyARN   string `json:"execution_policy_arn"`
	LogGroupName         string `json:"log_group_name"`
	// ConfigSecretARN is the managed secret holding the Rawtree API key plus
	// any inline DATABASE_URL / POSTGRES_TLS_ROOT_CERT_PEM as JSON keys.
	ConfigSecretARN  string `json:"config_secret_arn"`
	ConfigSecretName string `json:"config_secret_name"`

	// InitTaskDefinitionARN is set transiently during Create when the
	// initialization task is registered, and cleared after it deregisters
	// successfully. Tracked here so the rollback path can deregister an
	// orphaned init task definition on partial-create failure. Never
	// persisted to Terraform state — the success path always clears it.
	InitTaskDefinitionARN string `json:"-"`
}
