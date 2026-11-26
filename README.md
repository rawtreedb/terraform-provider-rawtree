# Terraform Provider for RawTree

Use this provider to manage [RawTree](https://rawtree.com) databases, tables, API keys, workflows, clusters and connectors, and to build AWS pipelines that ingest data into RawTree.

## Resources

| Resource | Description | Credential |
|----------|-------------|------------|
| [`rawtree_database`](docs/resources/database.md) | Database, optionally on your own S3 bucket | API key |
| [`rawtree_table`](docs/resources/table.md) | Table with sorting key, TTL and settings | API key |
| [`rawtree_api_key`](docs/resources/api_key.md) | API key with a permission level or database roles | API key |
| [`rawtree_workflow`](docs/resources/workflow.md) | Scheduled SQL query with table, HTTP and Slack sinks | API key |
| [`rawtree_cluster`](docs/resources/cluster.md) | Cluster in an organization region | Access token (interactive use only) |
| [`rawtree_connector`](docs/resources/connector.md) | Kafka, Postgres or NATS connector | Access token (interactive use only) |
| [`rawtree_s3_ingestion`](docs/resources/s3_ingestion.md) | Batch + streaming ingestion from S3 via Glue, Lambda, and EventBridge | API key |
| [`rawtree_supabase_cdc_ingestion`](docs/resources/supabase_cdc_ingestion.md) | Supabase Postgres CDC ingestion via ECS Fargate | API key |
| [`rawtree_waf_ingestion`](docs/resources/waf_ingestion.md) | Real-time AWS WAF log streaming via Kinesis Data Firehose | API key |
| [`rawtree_cloudfront_ingestion`](docs/resources/cloudfront_ingestion.md) | Real-time CloudFront access log streaming via Kinesis and Firehose | API key |

## Features

### S3 Ingestion (`rawtree_s3_ingestion`)

- **Batch ingestion**: Automatically ingest all existing objects from an S3 bucket/prefix into a Rawtree table using AWS Glue
- **Ongoing ingestion**: Set up EventBridge + Lambda to automatically ingest new objects as they arrive
- **Format support**: JSON, CSV, and Parquet (including gzipped variants)
- **Presigned URL ingestion**: Files are ingested via presigned URLs — Rawtree downloads directly from S3, no data passes through Glue/Lambda

### WAF Ingestion (`rawtree_waf_ingestion`)

- **Real-time streaming**: Stream AWS WAF logs directly to Rawtree via Kinesis Data Firehose
- **Zero code**: No Lambda or Glue required — Firehose delivers to the Rawtree HTTP endpoint
- **S3 backup**: Failed deliveries are automatically backed up with configurable retention
- **Configurable buffering**: Tune delivery latency vs. throughput with buffer size and interval settings

### CloudFront Ingestion (`rawtree_cloudfront_ingestion`)

- **Real-time streaming**: Stream CloudFront access logs to Rawtree via Kinesis Data Stream and Firehose
- **Configurable fields**: Choose which CloudFront log fields to include (20 recommended fields by default)
- **Sampling**: Control the percentage of requests that generate log records (1-100%)
- **S3 backup**: Failed deliveries are automatically backed up with a 30-day lifecycle policy

### Supabase CDC Ingestion (`rawtree_supabase_cdc_ingestion`)

- **Long-running CDC**: Run the Rawtree Supabase ETL worker as a single ECS Fargate service
- **Managed runtime**: Creates ECS, IAM, CloudWatch Logs, and Secrets Manager resources
- **Secret-aware**: Supports existing Secrets Manager ARNs for the Supabase database URL and CA certificate
- **Configurable sizing**: Tune Fargate CPU and memory with validated combinations

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0 (>= 1.11 for `rawtree_connector` credentials, which are write-only)
- [Go](https://golang.org/doc/install) >= 1.26 (to build the provider)
- AWS credentials with the [required permissions](#required-aws-permissions) (ingestion resources only)
- A [RawTree](https://rawtree.com) account with an API key (`rt_...`), and an access token for the access-token resources

## Installation

```hcl
terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}
```

## Authentication

RawTree has two credential types:

- **API key** (`rt_...`): the key selects its organization, cluster and default database. Changes to databases, API keys and workflows need an `admin` key.
- **Access token** (user OAuth access token or session token): required for clusters, connectors, service accounts and federation. A user OAuth token expires after 15 minutes and the provider does not refresh it. Workload federation tokens are not supported, so these resources are for interactive use only: run them from a workstation, not in CI.

The provider resolves each value in this order:

1. Provider configuration block
2. Environment variables (`RAWTREE_API_KEY`, `RAWTREE_ACCESS_TOKEN`, `RAWTREE_API_URL`, `RAWTREE_ORG`, `RAWTREE_CLUSTER`, `RAWTREE_DATABASE`)
3. rtree CLI config file (`~/.config/rtree/config.json`, created by `rtree login`)

```hcl
provider "rawtree" {
  api_key  = var.rawtree_api_key # or set RAWTREE_API_KEY
  database = "analytics"         # optional, defaults to the API key's database
}

provider "rawtree" {
  alias        = "admin"
  access_token = var.rawtree_access_token # or set RAWTREE_ACCESS_TOKEN
  organization = "my-org"                 # or set RAWTREE_ORG
  cluster      = "production"             # or set RAWTREE_CLUSTER
}
```

### Upgrade from 0.1

RawTree no longer has projects. Remove `project` from the provider block, and remove `organization` and `project` from the ingestion resources. Set `database` if you do not use the API key's database. The first `terraform apply` updates each WAF, CloudFront and S3 pipeline in place to the new ingest URL. It does not replace AWS resources. `rawtree_supabase_cdc_ingestion` still reads the deprecated `project` value.

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
    "glue:CreateJob", "glue:DeleteJob", "glue:GetJob", "glue:GetJobRun",
    "glue:GetJobRuns", "glue:StartJobRun", "glue:BatchStopJobRun",
    "lambda:CreateFunction", "lambda:DeleteFunction", "lambda:GetFunction",
    "lambda:UpdateFunctionConfiguration", "lambda:AddPermission",
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

# Acceptance tests (requires AWS + Rawtree credentials)
# See test/README.md for setup instructions
TF_ACC=1 go test -v -timeout 30m ./internal/resources/s3_ingestion/
TF_ACC=1 go test -v -timeout 30m ./internal/resources/waf_ingestion/
TF_ACC=1 go test -v -timeout 30m ./internal/resources/cloudfront_ingestion/
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
