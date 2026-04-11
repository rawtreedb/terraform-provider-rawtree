# Terraform Provider for Rawtree

The Rawtree Terraform provider enables automated data ingestion from AWS S3 into [Rawtree](https://rawtree.dev), a schemaless analytical database.

## Features

- **Batch ingestion**: Automatically ingest all existing objects from an S3 bucket/prefix into a Rawtree table using AWS Glue
- **Ongoing ingestion**: Set up EventBridge + Lambda to automatically ingest new objects as they arrive
- **Format support**: JSON, CSV, and Parquet (including gzipped variants)
- **Presigned URL ingestion**: Files are ingested via presigned URLs — Rawtree downloads directly from S3, no data passes through Glue/Lambda

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

## Usage

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

### How it works

On `terraform apply`, the provider creates AWS resources in your account to handle two ingestion flows:

1. **Batch (existing data)**: An AWS Glue Python Shell job lists all matching files under the specified bucket/prefix, generates presigned S3 URLs, and sends them to the Rawtree API. Rawtree downloads the files directly from S3 — no data passes through the Glue job. The job runs once at creation time and processes files concurrently.

2. **Streaming (new data)**: An EventBridge rule monitors the S3 bucket for `ObjectCreated` events and triggers a Lambda function. The Lambda generates a presigned URL for the new object and sends it to Rawtree for ingestion. This runs automatically for every new matching file.

```
                         ┌─────────────────────────────────────┐
                         │          Your AWS Account           │
                         │                                     │
                         │  ┌──────────┐    presigned URL      │
  Existing files ───────►│  │ Glue Job │──────────────────┐    │
                         │  └──────────┘                  │    │
                         │                                ▼    │
                         │  ┌──────────┐              ┌──────┐ │
  S3 ─► EventBridge ────►│  │  Lambda  │─── URL ────► │Rawtree│ │
        (ObjectCreated)  │  └──────────┘              │  API  │ │
                         │                            └───┬──┘ │
                         └────────────────────────────────┼────┘
                                                          │
                                           Rawtree downloads
                                           directly from S3
                                           via presigned URL
```

### AWS resources created

The provider creates and manages the following resources in your AWS account. All resources are tagged with `managed-by: terraform-provider-rawtree` and fully cleaned up on `terraform destroy`.

| Resource | Name pattern | Purpose |
|----------|-------------|---------|
| IAM Role | `rawtree-glue-{id}` | Allows the Glue job to read objects from your S3 bucket and generate presigned URLs |
| IAM Policy | `rawtree-glue-s3-{id}` | Scoped S3 read access (`GetObject`, `ListBucket`, `GetBucketLocation`) for the Glue role |
| IAM Role | `rawtree-lambda-{id}` | Allows the Lambda function to read objects from your S3 bucket and generate presigned URLs |
| IAM Policy | `rawtree-lambda-s3-{id}` | Scoped S3 read access (`GetObject`, `GetBucketLocation`) for the Lambda role |
| S3 Object | `.rawtree/glue-scripts/{id}/glue_job.py` | Glue job script stored in the source bucket (excluded from ingestion) |
| Glue Job | `rawtree-ingest-{id}` | Python Shell job (0.0625 DPU) for one-time batch ingestion |
| Lambda Function | `rawtree-ingest-{id}` | Python 3.12, 256 MB, handles per-object streaming ingestion |
| EventBridge Rule | `rawtree-s3-{id}` | Matches `ObjectCreated` events on the source bucket/prefix |

The `{id}` is derived from `{org}-{project}-{table}` with a short hash suffix for uniqueness.

### Resource arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `table` | Yes | Rawtree table name. Created automatically on first insert. |
| `bucket` | Yes | Source S3 bucket name. Changing this forces resource replacement. |
| `format` | Yes | File format: `json`, `csv`, or `parquet`. Changing this forces replacement. |
| `region` | Yes | AWS region for all created resources. Changing this forces replacement. |
| `prefix` | No | S3 key prefix filter (e.g. `data/events/`). Changing this forces replacement. |
| `file_pattern` | No | Regex pattern to filter object keys (e.g. `.*\.json$`). |

### Computed attributes

| Attribute | Description |
|-----------|-------------|
| `id` | Resource identifier |
| `glue_job_name` | Name of the created Glue job |
| `glue_job_run_id` | Run ID of the initial batch ingestion |
| `lambda_function_arn` | ARN of the ingestion Lambda function |
| `eventbridge_rule_arn` | ARN of the EventBridge rule |

## Required AWS permissions

The AWS credentials used to run Terraform must have the following permissions. You can use the policy below as a starting point — adjust the `Resource` fields to match your bucket and account.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "IAMRolesAndPolicies",
      "Effect": "Allow",
      "Action": [
        "iam:CreateRole",
        "iam:DeleteRole",
        "iam:AttachRolePolicy",
        "iam:DetachRolePolicy",
        "iam:CreatePolicy",
        "iam:DeletePolicy",
        "iam:GetRole",
        "iam:PassRole"
      ],
      "Resource": [
        "arn:aws:iam::*:role/rawtree-*",
        "arn:aws:iam::*:policy/rawtree-*"
      ]
    },
    {
      "Sid": "GlueJobs",
      "Effect": "Allow",
      "Action": [
        "glue:CreateJob",
        "glue:DeleteJob",
        "glue:GetJob",
        "glue:GetJobRun",
        "glue:GetJobRuns",
        "glue:StartJobRun",
        "glue:BatchStopJobRun"
      ],
      "Resource": "arn:aws:glue:*:*:job/rawtree-ingest-*"
    },
    {
      "Sid": "LambdaFunctions",
      "Effect": "Allow",
      "Action": [
        "lambda:CreateFunction",
        "lambda:DeleteFunction",
        "lambda:GetFunction",
        "lambda:UpdateFunctionConfiguration",
        "lambda:AddPermission"
      ],
      "Resource": "arn:aws:lambda:*:*:function:rawtree-ingest-*"
    },
    {
      "Sid": "EventBridgeRules",
      "Effect": "Allow",
      "Action": [
        "events:PutRule",
        "events:DeleteRule",
        "events:DescribeRule",
        "events:PutTargets",
        "events:RemoveTargets"
      ],
      "Resource": "arn:aws:events:*:*:rule/rawtree-s3-*"
    },
    {
      "Sid": "S3ScriptAndNotifications",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:DeleteObject",
        "s3:HeadObject",
        "s3:PutBucketNotificationConfiguration",
        "s3:GetBucketNotificationConfiguration"
      ],
      "Resource": [
        "arn:aws:s3:::YOUR-BUCKET-NAME",
        "arn:aws:s3:::YOUR-BUCKET-NAME/.rawtree/*"
      ]
    }
  ]
}
```

> **Note**: The provider enables EventBridge notifications on your S3 bucket (`PutBucketNotificationConfiguration`). This is additive and does not remove existing notification configurations. The Glue job script is stored in the source bucket under the `.rawtree/` prefix, which is automatically excluded from ingestion.

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
