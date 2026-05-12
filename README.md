# Terraform Provider for Rawtree

The Rawtree Terraform provider enables automated data ingestion from AWS sources into [Rawtree](https://rawtree.dev), a schemaless analytical database.

## Resources

| Resource | Description | Docs |
|----------|-------------|------|
| [`rawtree_s3_ingestion`](docs/resources/s3_ingestion.md) | Batch + streaming ingestion from S3 via Glue, Lambda, and EventBridge | [Full docs](docs/resources/s3_ingestion.md) |
| [`rawtree_waf_ingestion`](docs/resources/waf_ingestion.md) | Real-time AWS WAF log streaming via Kinesis Data Firehose | [Full docs](docs/resources/waf_ingestion.md) |

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

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.22 (to build the provider)
- AWS credentials with the [required permissions](#required-aws-permissions)
- A [Rawtree](https://rawtree.dev) account with an API key

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

The provider resolves credentials in this order:

1. Provider configuration block
2. Environment variables (`RAWTREE_API_KEY`, `RAWTREE_URL`, `RAWTREE_ORG`, `RAWTREE_PROJECT`)
3. Rawtree CLI config file (`~/.config/rtree/config.json` — created by `rtree login`)

```hcl
provider "rawtree" {
  api_key      = var.rawtree_api_key    # or set RAWTREE_API_KEY
  organization = "my-org"               # or set RAWTREE_ORG
  project      = "my-project"           # or set RAWTREE_PROJECT
}
```

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

See the full resource documentation for detailed schema, AWS resources created, and configuration options:
- [`rawtree_s3_ingestion`](docs/resources/s3_ingestion.md)
- [`rawtree_waf_ingestion`](docs/resources/waf_ingestion.md)

## Required AWS Permissions

The AWS credentials used to run Terraform need different permissions depending on which resources you use.

### Common (both resources)

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
