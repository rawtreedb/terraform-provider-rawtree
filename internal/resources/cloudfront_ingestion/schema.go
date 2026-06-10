package cloudfront_ingestion

import (
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// canonicalFieldOrder maps every CloudFront real-time log field to its
// position in the canonical delivery order documented by AWS. CloudFront
// always delivers TSV values in this order regardless of the order
// specified in CreateRealtimeLogConfig.
// https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/real-time-logs.html#real-time-logs-fields
var canonicalFieldOrder = map[string]int{
	"timestamp":                         1,
	"c-ip":                              2,
	"s-ip":                              3,
	"time-to-first-byte":                4,
	"sc-status":                         5,
	"sc-bytes":                          6,
	"cs-method":                         7,
	"cs-protocol":                       8,
	"cs-host":                           9,
	"cs-uri-stem":                       10,
	"cs-bytes":                          11,
	"x-edge-location":                   12,
	"x-edge-request-id":                 13,
	"x-host-header":                     14,
	"time-taken":                        15,
	"cs-protocol-version":               16,
	"c-ip-version":                      17,
	"cs-user-agent":                     18,
	"cs-referer":                        19,
	"cs-cookie":                         20,
	"cs-uri-query":                      21,
	"x-edge-response-result-type":       22,
	"x-forwarded-for":                   23,
	"ssl-protocol":                      24,
	"ssl-cipher":                        25,
	"x-edge-result-type":                26,
	"fle-encrypted-fields":              27,
	"fle-status":                        28,
	"sc-content-type":                   29,
	"sc-content-len":                    30,
	"sc-range-start":                    31,
	"sc-range-end":                      32,
	"c-port":                            33,
	"x-edge-detailed-result-type":       34,
	"c-country":                         35,
	"cs-accept-encoding":                36,
	"cs-accept":                         37,
	"cache-behavior-path-pattern":       38,
	"cs-headers":                        39,
	"cs-header-names":                   40,
	"cs-headers-count":                  41,
	"primary-distribution-id":           42,
	"primary-distribution-dns-name":     43,
	"origin-fbl":                        44,
	"origin-lbl":                        45,
	"asn":                               46,
	"cmcd-encoded-bitrate":              47,
	"cmcd-buffer-length":                48,
	"cmcd-buffer-starvation":            49,
	"cmcd-content-id":                   50,
	"cmcd-object-duration":              51,
	"cmcd-deadline":                     52,
	"cmcd-measured-throughput":          53,
	"cmcd-next-object-request":          54,
	"cmcd-next-range-request":           55,
	"cmcd-object-type":                  56,
	"cmcd-playback-rate":                57,
	"cmcd-requested-maximum-throughput": 58,
	"cmcd-streaming-format":             59,
	"cmcd-session-id":                   60,
	"cmcd-stream-type":                  61,
	"cmcd-startup":                      62,
	"cmcd-top-bitrate":                  63,
	"cmcd-version":                      64,
	"r-host":                            65,
	"sr-reason":                         66,
	"x-edge-mqcs":                       67,
	"distribution-tenant-id":            68,
	"connection-id":                     69,
}

func sortFieldsCanonical(fields []string) []string {
	sorted := make([]string, len(fields))
	copy(sorted, fields)
	sort.SliceStable(sorted, func(i, j int) bool {
		oi, iKnown := canonicalFieldOrder[sorted[i]]
		oj, jKnown := canonicalFieldOrder[sorted[j]]
		if !iKnown {
			oi = len(canonicalFieldOrder) + 1
		}
		if !jKnown {
			oj = len(canonicalFieldOrder) + 1
		}
		return oi < oj
	})
	return sorted
}

var defaultFields = []string{
	"timestamp", "c-ip", "time-to-first-byte", "sc-status", "sc-bytes",
	"cs-method", "cs-protocol", "cs-host", "cs-uri-stem", "cs-bytes",
	"x-edge-location", "time-taken", "cs-protocol-version", "cs-user-agent",
	"x-edge-response-result-type", "ssl-protocol", "x-edge-result-type",
	"c-port", "x-edge-detailed-result-type", "c-country",
}

func defaultFieldsListValue() types.List {
	elems := make([]attr.Value, len(defaultFields))
	for i, f := range defaultFields {
		elems[i] = types.StringValue(f)
	}
	return types.ListValueMust(types.StringType, elems)
}

func resourceSchema() schema.Schema {
	defaultList := defaultFieldsListValue()

	return schema.Schema{
		Description: "Manages real-time CloudFront log ingestion into Rawtree. Creates a Kinesis Data Stream, " +
			"Kinesis Data Firehose delivery stream with HTTP endpoint destination, and a CloudFront real-time " +
			"log configuration to stream CloudFront access logs to Rawtree, with S3 backup for failed deliveries.",
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
				Description: "The Rawtree table name to ingest CloudFront logs into. Will be auto-created on first insert.",
			},
			"distribution_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the CloudFront distribution to attach real-time logging to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Required:    true,
				Description: "AWS region where the Kinesis Data Stream, Firehose delivery stream, and backup bucket will be created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"sampling_rate": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(100),
				Description: "Percentage of requests to log. Valid range: 1-100. Default: 100 (all requests).",
				Validators: []validator.Int64{
					int64validator.Between(1, 100),
				},
			},
			"fields": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default:     listdefault.StaticValue(defaultList),
				Description: "CloudFront real-time log fields to include, in CloudFront canonical order. " +
					"Order matters: CloudFront delivers TSV values positionally, and the columns= URL " +
					"parameter maps positions to names. Defaults to 20 recommended fields.",
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
				Description: "The full Firehose HTTP endpoint URL.",
			},

			"kinesis_stream_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the Kinesis Data Stream.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"kinesis_stream_name": schema.StringAttribute{
				Computed:    true,
				Description: "The name of the Kinesis Data Stream.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
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
			"realtime_log_config_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The ARN of the CloudFront real-time log configuration.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}
