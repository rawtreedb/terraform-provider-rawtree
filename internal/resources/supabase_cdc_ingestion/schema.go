package supabase_cdc_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("1"),
				Description: "supabase/etl pipeline identifier. The default produces replication slot supabase_etl_apply_1.",
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
				Default:     booldefault.StaticBool(true),
				Description: "Run a one-off ECS task before starting the service to validate/setup the ETL pipeline. Default: true.",
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
			"rawtree_secret_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The managed Secrets Manager secret ARN containing the Rawtree API key.",
			},
		},
	}
}
