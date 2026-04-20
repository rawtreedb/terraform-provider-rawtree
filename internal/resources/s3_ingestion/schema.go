package s3_ingestion

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func resourceSchema() schema.Schema {
	return schema.Schema{
		Description: "Manages S3 data ingestion into Rawtree. Creates AWS Glue job for batch ingestion " +
			"of existing objects and Lambda + EventBridge for ongoing ingestion of new objects.",
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
				Description: "The Rawtree table name to ingest data into. Will be auto-created on first insert.",
			},
			"bucket": schema.StringAttribute{
				Required:    true,
				Description: "The S3 bucket name containing source data.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"prefix": schema.StringAttribute{
				Optional:    true,
				Description: "S3 key prefix to filter objects. Only objects under this prefix will be ingested.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"file_pattern": schema.StringAttribute{
				Optional:    true,
				Description: "Regular expression pattern to filter object keys. Only matching files will be ingested.",
			},
			"format": schema.StringAttribute{
				Required:    true,
				Description: "File format of the source data. Supported: parquet, csv, json.",
				Validators: []validator.String{
					stringvalidator.OneOf("parquet", "csv", "json"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Required:    true,
				Description: "AWS region where Glue, Lambda, and EventBridge resources will be created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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

			// Provider-derived attributes (trigger update when provider config changes).
			"api_url": schema.StringAttribute{
				Computed:    true,
				Description: "The Rawtree API URL (from provider config).",
			},
			"api_key_hash": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Hash of the API key (from provider config). Changes trigger Lambda env var update.",
			},

			// Computed attributes.
			"glue_job_name": schema.StringAttribute{
				Computed:    true,
				Description: "The name of the AWS Glue job created for batch ingestion.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"glue_job_run_id": schema.StringAttribute{
				Computed:    true,
				Description: "The run ID of the initial Glue job execution.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"lambda_function_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the Lambda function created for ongoing ingestion.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"eventbridge_rule_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the EventBridge rule created for S3 event monitoring.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}
