---
page_title: "rawtree_s3_ingestion Resource - Rawtree"
subcategory: ""
description: |-
  Manages S3 data ingestion into Rawtree.
---

# rawtree_s3_ingestion (Resource)

Manages S3 data ingestion into Rawtree. Creates an AWS Glue job for batch ingestion of existing objects and a Lambda function with EventBridge for ongoing ingestion of new objects.

## How It Works

1. **Batch ingestion** (on `terraform apply`): A Glue job lists all matching files in the specified S3 bucket/prefix, generates presigned URLs, and sends them to the Rawtree API. Rawtree downloads each file directly from S3.

2. **Ongoing ingestion** (continuous): An EventBridge rule triggers a Lambda function whenever a new object is created in the bucket. The Lambda generates a presigned URL and sends it to Rawtree for ingestion.

## Example Usage

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

## Schema

### Required

- `table` (String) - The Rawtree table name to ingest data into. Will be auto-created on first insert.
- `bucket` (String) - The S3 bucket name containing source data. Changing this forces a new resource.
- `format` (String) - File format of the source data. One of: `parquet`, `csv`, `json`. Changing this forces a new resource.
- `region` (String) - AWS region where Glue, Lambda, and EventBridge resources will be created. Changing this forces a new resource.

### Optional

- `prefix` (String) - S3 key prefix to filter objects. Changing this forces a new resource.
- `file_pattern` (String) - Regular expression pattern to filter object keys.

### Read-Only

- `id` (String) - The unique identifier for this ingestion resource.
- `glue_job_name` (String) - The name of the AWS Glue job created for batch ingestion.
- `glue_job_run_id` (String) - The run ID of the initial Glue job execution.
- `lambda_function_arn` (String) - The ARN of the Lambda function created for ongoing ingestion.
- `eventbridge_rule_arn` (String) - The ARN of the EventBridge rule created for S3 event monitoring.

## AWS Resources Created

| Resource | Name Pattern | Purpose |
|----------|-------------|---------|
| IAM Role | `rawtree-glue-{name}` | Glue job execution role with S3 read access |
| IAM Role | `rawtree-lambda-{name}` | Lambda execution role with S3 read access |
| S3 Bucket | `rawtree-glue-scripts-{name}` | Stores the Glue job Python script |
| Glue Job | `rawtree-ingest-{name}` | One-time batch ingestion |
| Lambda Function | `rawtree-ingest-{name}` | Per-object ongoing ingestion |
| EventBridge Rule | `rawtree-s3-{name}` | S3 ObjectCreated event matching |

All resources are tagged with `managed-by: terraform-provider-rawtree`.

## Import

Import is not supported. Please create the resource using Terraform.
