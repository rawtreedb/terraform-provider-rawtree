# Terraform Provider for Tinybird RawTree

Use Terraform to manage Tinybird RawTree databases and external ingestion pipelines.

## Tinybird RawTree

[`rawtree_database`](docs/resources/database.md) manages a named database in an existing cluster. Use the [database list resource](docs/list-resources/database.md) to generate Terraform configuration for the databases already there.

[`rawtree_workflow`](docs/resources/workflow.md) manages a SQL workflow and its schedule. The [workflow list resource](docs/list-resources/workflow.md) generates configuration for existing workflows.

## Integrations

| Resource | Description | Docs |
|----------|-------------|------|
| [`rawtree_s3_ingestion`](docs/resources/s3_ingestion.md) | Batch + streaming ingestion from S3 via Glue, Lambda, and EventBridge | [Full docs](docs/resources/s3_ingestion.md) |
| [`rawtree_supabase_cdc_ingestion`](docs/resources/supabase_cdc_ingestion.md) | Supabase Postgres CDC ingestion via ECS Fargate | [Full docs](docs/resources/supabase_cdc_ingestion.md) |
| [`rawtree_waf_ingestion`](docs/resources/waf_ingestion.md) | Real-time AWS WAF log streaming via Kinesis Data Firehose | [Full docs](docs/resources/waf_ingestion.md) |
| [`rawtree_cloudfront_ingestion`](docs/resources/cloudfront_ingestion.md) | Real-time CloudFront access log streaming via Kinesis and Firehose | [Full docs](docs/resources/cloudfront_ingestion.md) |

## Features

### S3 Ingestion (`rawtree_s3_ingestion`)

- **Batch ingestion**: Automatically ingest all existing objects from an S3 bucket/prefix into a Tinybird RawTree table using AWS Glue
- **Ongoing ingestion**: Set up EventBridge + Lambda to automatically ingest new objects as they arrive
- **Format support**: JSON, CSV, and Parquet (including gzipped variants)
- **Presigned URL ingestion**: Files are ingested via presigned URLs — Tinybird RawTree downloads directly from S3, no data passes through Glue/Lambda

### WAF Ingestion (`rawtree_waf_ingestion`)

- **Real-time streaming**: Stream AWS WAF logs directly to Tinybird RawTree via Kinesis Data Firehose
- **Zero code**: No Lambda or Glue required — Firehose delivers to the Tinybird RawTree HTTP endpoint
- **S3 backup**: Failed deliveries are automatically backed up with configurable retention
- **Configurable buffering**: Tune delivery latency vs. throughput with buffer size and interval settings

### CloudFront Ingestion (`rawtree_cloudfront_ingestion`)

- **Real-time streaming**: Stream CloudFront access logs to Tinybird RawTree via Kinesis Data Stream and Firehose
- **Configurable fields**: Choose which CloudFront log fields to include (20 recommended fields by default)
- **Sampling**: Control the percentage of requests that generate log records (1-100%)
- **S3 backup**: Failed deliveries are automatically backed up with a 30-day lifecycle policy

### Supabase CDC Ingestion (`rawtree_supabase_cdc_ingestion`)

- **Long-running CDC**: Run the Tinybird RawTree Supabase ETL worker as a single ECS Fargate service
- **Managed runtime**: Creates ECS, IAM, CloudWatch Logs, and Secrets Manager resources
- **Secret-aware**: Supports existing Secrets Manager ARNs for the Supabase database URL and CA certificate
- **Configurable sizing**: Tune Fargate CPU and memory with validated combinations

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.26.1 (to build the provider)
- For ingestion integrations, AWS credentials with the [required permissions](#required-aws-permissions)
- A [Tinybird RawTree](https://rawtree.dev) account with an API key

## Installation

```hcl
terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}
```

## Authentication

For native resources, set `RAWTREE_MANAGEMENT_TOKEN` or the sensitive `management_token` provider setting.
This credential stays in the provider. Native resources can also use the existing API key when it has database administration access.
Specify the organization and cluster on the database resource. Its [example](examples/resources/database/main.tf) requires no AWS credentials.

For ingestion integrations, the provider resolves settings in this order:

1. Provider configuration block
2. Environment variables (`RAWTREE_API_KEY`, `RAWTREE_URL`, `RAWTREE_ORG`, `RAWTREE_DATABASE`)
3. Tinybird RawTree CLI config file (`~/.config/rtree/config.json` — created by `rtree login`)

```hcl
provider "rawtree" {
  api_key      = var.rawtree_api_key    # or set RAWTREE_API_KEY
  organization = "my-org"               # or set RAWTREE_ORG
  database     = "my_database"          # or set RAWTREE_DATABASE
}
```

Existing configurations can keep the deprecated `project` alias or `RAWTREE_PROJECT`. The integration uses the ingestion key to select the organization and cluster. Apply the in-place endpoint updates when upgrading an existing deployment.

## Quick Start

### S3 Ingestion

```hcl
resource "rawtree_s3_ingestion" "events" {
  table        = "events"
  bucket       = "my-data-bucket"
  prefix       = "data/events/"
  file_pattern = ".*\\.json$"
  format       = "json"
  region       = "us-east-1"
}
```

### WAF Log Ingestion

```hcl
resource "rawtree_waf_ingestion" "waf_logs" {
  table       = "waf_logs"
  web_acl_arn = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/abc123"
  region      = "us-east-1"
}
```

### CloudFront Log Ingestion

```hcl
resource "rawtree_cloudfront_ingestion" "logs" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"
}
```

### Supabase CDC Ingestion

```hcl
resource "rawtree_supabase_cdc_ingestion" "orders" {
  name        = "orders"
  region      = "us-east-1"
  publication = "rawtree_publication"

  database_url_secret_arn = aws_secretsmanager_secret.supabase_database_url.arn
  tls_root_cert_secret_arn = aws_secretsmanager_secret.supabase_ca.arn

  subnet_ids         = var.private_subnet_ids
  security_group_ids = [aws_security_group.rawtree_supabase_cdc.id]

  cpu    = 512
  memory = 1024
}
```

See the full resource documentation for detailed schema, AWS resources created, and configuration options:
- [`rawtree_s3_ingestion`](docs/resources/s3_ingestion.md)
- [`rawtree_supabase_cdc_ingestion`](docs/resources/supabase_cdc_ingestion.md)
- [`rawtree_waf_ingestion`](docs/resources/waf_ingestion.md)
- [`rawtree_cloudfront_ingestion`](docs/resources/cloudfront_ingestion.md)

## Required AWS Permissions

The AWS credentials used to run Terraform need different permissions depending on which resources you use.

### Common

```json
{
  "Sid": "IAMRolesAndPolicies",
  "Effect": "Allow",
  "Action": [
    "iam:CreateRole", "iam:DeleteRole", "iam:GetRole", "iam:PassRole",
    "iam:AttachRolePolicy", "iam:DetachRolePolicy",
    "iam:CreatePolicy", "iam:DeletePolicy"
  ],
  "Resource": [
    "arn:aws:iam::*:role/rawtree-*",
    "arn:aws:iam::*:policy/rawtree-*"
  ]
}
```

### For `rawtree_s3_ingestion`

Glue, Lambda, EventBridge, and S3 permissions. See [`docs/resources/s3_ingestion.md`](docs/resources/s3_ingestion.md) for the full list of AWS resources created.

```json
{
  "Sid": "S3Ingestion",
  "Effect": "Allow",
  "Action": [
    "glue:CreateJob", "glue:UpdateJob", "glue:DeleteJob", "glue:GetJob", "glue:GetJobRun",
    "glue:GetJobRuns", "glue:StartJobRun", "glue:BatchStopJobRun",
    "lambda:CreateFunction", "lambda:DeleteFunction", "lambda:GetFunction",
    "lambda:GetFunctionConfiguration", "lambda:UpdateFunctionCode", "lambda:UpdateFunctionConfiguration", "lambda:AddPermission",
    "events:PutRule", "events:DeleteRule", "events:DescribeRule",
    "events:PutTargets", "events:RemoveTargets",
    "s3:PutObject", "s3:GetObject", "s3:DeleteObject", "s3:HeadObject",
    "s3:PutBucketNotificationConfiguration", "s3:GetBucketNotificationConfiguration"
  ],
  "Resource": "*"
}
```

### For `rawtree_waf_ingestion`

Firehose, WAFv2, and S3 permissions. See [`docs/resources/waf_ingestion.md`](docs/resources/waf_ingestion.md) for the full list of AWS resources created.

```json
{
  "Sid": "WafIngestion",
  "Effect": "Allow",
  "Action": [
    "firehose:CreateDeliveryStream", "firehose:DeleteDeliveryStream",
    "firehose:DescribeDeliveryStream", "firehose:UpdateDestination",
    "firehose:TagDeliveryStream",
    "wafv2:PutLoggingConfiguration", "wafv2:DeleteLoggingConfiguration",
    "wafv2:GetLoggingConfiguration",
    "s3:CreateBucket", "s3:DeleteBucket", "s3:HeadBucket",
    "s3:PutBucketLifecycleConfiguration", "s3:PutBucketTagging",
    "s3:ListBucket", "s3:DeleteObject", "s3:ListBucketMultipartUploads"
  ],
  "Resource": "*"
}
```

### For `rawtree_cloudfront_ingestion`

Kinesis, Firehose, CloudFront, and S3 permissions. See [`docs/resources/cloudfront_ingestion.md`](docs/resources/cloudfront_ingestion.md) for the full list of AWS resources created.

```json
{
  "Sid": "CloudFrontIngestion",
  "Effect": "Allow",
  "Action": [
    "kinesis:CreateStream", "kinesis:DeleteStream", "kinesis:DescribeStreamSummary",
    "firehose:CreateDeliveryStream", "firehose:DeleteDeliveryStream",
    "firehose:DescribeDeliveryStream", "firehose:UpdateDestination",
    "cloudfront:CreateRealtimeLogConfig", "cloudfront:DeleteRealtimeLogConfig",
    "cloudfront:GetRealtimeLogConfig", "cloudfront:UpdateRealtimeLogConfig",
    "cloudfront:GetDistributionConfig", "cloudfront:UpdateDistribution",
    "s3:CreateBucket", "s3:DeleteBucket", "s3:HeadBucket",
    "s3:PutBucketLifecycleConfiguration", "s3:PutBucketTagging",
    "s3:ListBucket", "s3:DeleteObject", "s3:ListBucketMultipartUploads"
  ],
  "Resource": "*"
}
```

### For `rawtree_supabase_cdc_ingestion`

ECS, CloudWatch Logs, Secrets Manager, and IAM permissions. See [`docs/resources/supabase_cdc_ingestion.md`](docs/resources/supabase_cdc_ingestion.md) for the full list of AWS resources created.

```json
{
  "Sid": "SupabaseCDCIngestion",
  "Effect": "Allow",
  "Action": [
    "ecs:CreateCluster", "ecs:DeleteCluster",
    "ecs:RegisterTaskDefinition", "ecs:DeregisterTaskDefinition",
    "ecs:CreateService", "ecs:UpdateService", "ecs:DeleteService",
    "ecs:DescribeServices", "ecs:RunTask", "ecs:DescribeTasks", "ecs:TagResource",
    "logs:CreateLogGroup", "logs:DeleteLogGroup", "logs:PutRetentionPolicy", "logs:TagResource",
    "secretsmanager:CreateSecret", "secretsmanager:PutSecretValue",
    "secretsmanager:DeleteSecret", "secretsmanager:GetSecretValue", "secretsmanager:TagResource"
  ],
  "Resource": "*"
}
```

> **Tip**: Scope `Resource` to specific ARNs in production. The examples above use `*` for simplicity.

## Development

### Building

```sh
go build -v ./...
```

### Testing

```sh
# Unit tests
go test -v ./...

# Acceptance tests (requires AWS + Tinybird RawTree credentials)
# See test/README.md for setup instructions
TF_ACC=1 go test -v -timeout 30m ./internal/resources/integrations/s3_ingestion/
TF_ACC=1 go test -v -timeout 30m ./internal/resources/integrations/waf_ingestion/
TF_ACC=1 go test -v -timeout 30m ./internal/resources/integrations/cloudfront_ingestion/
```

### Using a local build

Add this to your `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "rawtreedb/rawtree" = "/path/to/terraform-provider-rawtree"
  }
  direct {}
}
```

## License

[Mozilla Public License v2.0](LICENSE)
