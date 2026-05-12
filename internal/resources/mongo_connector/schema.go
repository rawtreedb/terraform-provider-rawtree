package mongo_connector

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

func resourceSchema() schema.Schema {
	return schema.Schema{
		Description: "Deploys a MongoDB CDC connector on AWS ECS Fargate that replicates data from " +
			"MongoDB (replica set or Atlas) into Rawtree using Change Streams. Performs an initial " +
			"snapshot of existing data, then streams real-time inserts, updates, and deletes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique identifier for this connector resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"table": schema.StringAttribute{
				Required:    true,
				Description: "Base Rawtree table name prefix. Each MongoDB collection is mapped to {table_prefix}{collection}.",
			},
			"mongo_uri": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "MongoDB connection URI (mongodb:// or mongodb+srv://). Must point to a replica set.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"mongo_database": schema.StringAttribute{
				Required:    true,
				Description: "The MongoDB database to watch for changes.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Required:    true,
				Description: "AWS region where the ECS Fargate service will be deployed. Should be close to your MongoDB instance.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"collections": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Comma-separated list of MongoDB collections to watch. Empty means all collections.",
			},
			"table_prefix": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("mongo_"),
				Description: "Prefix for Rawtree table names. Each collection becomes {table_prefix}{collection}. Default: mongo_.",
			},
			"full_document": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("updateLookup"),
				Description: "Change stream fullDocument mode. Valid values: updateLookup, whenAvailable, required. Default: updateLookup.",
				Validators: []validator.String{
					stringvalidator.OneOf("updateLookup", "whenAvailable", "required"),
				},
			},
			"snapshot_enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether to perform an initial snapshot of existing data. Default: true.",
			},
			"batch_max_rows": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(4000),
				Description: "Maximum rows per batch sent to Rawtree. Valid range: 100-5000. Default: 4000.",
				Validators: []validator.Int64{
					int64validator.Between(100, 5000),
				},
			},
			"flush_interval": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("5s"),
				Description: "Maximum time before flushing a partial batch. Default: 5s.",
			},
			"image_tag": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("latest"),
				Description: "Docker image tag for the rawtree-mongo-connector. Default: latest.",
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
				Description: "Hash of the API key (from provider config). Changes trigger connector update.",
			},

			"ecs_cluster_arn": schema.StringAttribute{
				Computed:    true,
				Description: "ARN of the ECS cluster running the connector.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ecs_service_arn": schema.StringAttribute{
				Computed:    true,
				Description: "ARN of the ECS Fargate service.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"task_definition_arn": schema.StringAttribute{
				Computed:    true,
				Description: "ARN of the ECS task definition.",
			},
			"log_group_name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the CloudWatch log group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_arn": schema.StringAttribute{
				Computed:    true,
				Description: "ARN of the Secrets Manager secret storing connection credentials.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}
