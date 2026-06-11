package supabase_cdc_ingestion

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// pipelineIDPattern matches a positive integer with no leading zero (matches
// the u64 input the supabase/etl worker expects via env_u64("PIPELINE_ID")).
var pipelineIDPattern = regexp.MustCompile(`^([1-9][0-9]*)$`)

const defaultImage = "ghcr.io/rawtreedb/supabase-etl:latest"

func resourceSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages Supabase Postgres CDC ingestion into Rawtree using a single ECS Fargate service.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique identifier for this ingestion resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Stable name for this CDC worker. Used in AWS resource names.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Required:    true,
				Description: "AWS region where ECS, IAM, CloudWatch Logs, and managed secrets will be created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"publication": schema.StringAttribute{
				Required:    true,
				Description: "Postgres publication consumed by supabase/etl.",
			},
			"pipeline_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("1"),
				Description: "supabase/etl pipeline identifier. Must be a positive integer; the worker parses it as a u64 " +
					"and uses it as the suffix of the Postgres logical replication slot name (`supabase_etl_apply_<id>`). " +
					"Leave at the default (`1`) unless you intentionally want a second, independent replication stream — " +
					"the provider does not drop the slot on destroy, so changing the id leaks a slot in the source database " +
					"that pins WAL until you drop it manually with `pg_drop_replication_slot`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						pipelineIDPattern,
						"pipeline_id must be a positive integer (e.g. \"1\", \"2\"); the supabase/etl worker parses it as a u64",
					),
				},
			},
			"image": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(defaultImage),
				Description: "Container image for the Rawtree Supabase ETL worker.",
			},
			"cpu": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(512),
				Description: "Fargate task CPU units. Default: 512.",
				Validators: []validator.Int64{
					int64validator.OneOf(256, 512, 1024, 2048, 4096, 8192, 16384),
				},
			},
			"memory": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1024),
				Description: "Fargate task memory in MiB. Default: 1024.",
				Validators: []validator.Int64{
					int64validator.Between(512, 122880),
				},
			},
			"subnet_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Subnet IDs where the Fargate task should run. These subnets need outbound access to Supabase and Rawtree.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"security_group_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Security group IDs for the Fargate task. If omitted, ECS uses the VPC default security group.",
			},
			"assign_public_ip": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the Fargate task should receive a public IPv4 address. Default: false.",
			},
			"database_url": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Supabase direct Postgres URL. If set, the provider stores it in a managed Secrets Manager secret.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database_url_secret_arn": schema.StringAttribute{
				Optional:    true,
				Description: "ARN of an existing Secrets Manager secret containing the Supabase direct Postgres URL.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tls_root_cert_pem": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Supabase database CA certificate PEM. If set, the provider stores it in a managed Secrets Manager secret.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tls_root_cert_secret_arn": schema.StringAttribute{
				Optional:    true,
				Description: "ARN of an existing Secrets Manager secret containing the Supabase database CA certificate PEM.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"log_retention_days": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(30),
				Description: "CloudWatch Logs retention in days. Default: 30.",
				Validators: []validator.Int64{
					int64validator.OneOf(1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653),
				},
			},
			"run_initialization_task": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Run a short-lived one-off ECS task before starting the service. Only enable this if your image exposes a dedicated init subcommand that exits on success; the default image starts the long-running CDC pipeline and will time out. Default: false.",
			},
			"initialization_command": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "Command used for the optional one-off initialization task.",
			},
			"worker_command": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "Command used for the long-running worker container.",
			},
			"environment": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Additional non-sensitive environment variables passed to both init and worker containers.",
			},
			"organization": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The Rawtree organization. Defaults to the provider-level organization.",
			},
			"project": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The Rawtree project. Defaults to the provider-level project.",
			},
			"api_url": schema.StringAttribute{
				Computed:    true,
				Description: "The Rawtree API URL from provider config.",
			},
			"api_key_hash": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Hash of the Rawtree API key. Changes trigger a new task definition and service deployment.",
			},
			"cluster_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the ECS cluster.",
			},
			"service_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the ECS service.",
			},
			"task_definition_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the active ECS task definition.",
			},
			"log_group_name": schema.StringAttribute{
				Computed:    true,
				Description: "The CloudWatch Logs group used by the worker.",
			},
			"execution_role_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The IAM execution role used by ECS.",
			},
			"config_secret_arn": schema.StringAttribute{
				Computed:    true,
				Description: "ARN of the managed Secrets Manager secret holding the Rawtree API key — and, when supplied inline, the Supabase database URL and CA certificate — as JSON keys.",
			},
		},
	}
}
