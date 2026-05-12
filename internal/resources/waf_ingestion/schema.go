package waf_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func resourceSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages real-time AWS WAF log ingestion into Rawtree. Creates a Kinesis Data Firehose " +
			"delivery stream with HTTP endpoint destination to stream WAF logs from a Web ACL to Rawtree, " +
			"with S3 backup for failed deliveries.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique identifier for this ingestion resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"table": schema.StringAttribute{
				Required:    true,
				Description: "The Rawtree table name to ingest WAF logs into. Will be auto-created on first insert.",
			},
			"web_acl_arn": schema.StringAttribute{
				Required:    true,
				Description: "The ARN of the WAFv2 Web ACL to attach logging to. For CloudFront Web ACLs, the region must be us-east-1.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Required:    true,
				Description: "AWS region where the Firehose delivery stream and backup bucket will be created. Must match the Web ACL region (us-east-1 for CloudFront).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"buffering_size": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5),
				Description: "Firehose buffer size in MB before delivery. Valid range: 1-64. Default: 5.",
				Validators: []validator.Int64{
					int64validator.Between(1, 64),
				},
			},
			"buffering_interval": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(300),
				Description: "Firehose buffer interval in seconds before delivery. Valid range: 60-900. Default: 300.",
				Validators: []validator.Int64{
					int64validator.Between(60, 900),
				},
			},
			"s3_backup_mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("FailedDataOnly"),
				Description: "S3 backup mode for the Firehose delivery stream. Valid values: FailedDataOnly, AllData. Default: FailedDataOnly.",
				Validators: []validator.String{
					stringvalidator.OneOf("FailedDataOnly", "AllData"),
				},
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
				Description: "The Rawtree API URL (from provider config).",
			},
			"api_key_hash": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Hash of the API key (from provider config). Changes trigger Firehose destination update.",
			},
			"endpoint_url": schema.StringAttribute{
				Computed:    true,
				Description: "The full Firehose HTTP endpoint URL (e.g. {api_url}/v1/{org}/{project}/tables/{table}?transform=firehose).",
			},

			"firehose_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the Kinesis Data Firehose delivery stream.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"firehose_name": schema.StringAttribute{
				Computed:    true,
				Description: "The name of the Kinesis Data Firehose delivery stream.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"backup_bucket_name": schema.StringAttribute{
				Computed:    true,
				Description: "The name of the S3 bucket used for failed delivery backup.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"waf_logging_configuration_id": schema.StringAttribute{
				Computed:    true,
				Description: "The ID of the WAF logging configuration.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}
