# rawtree_cloudfront_ingestion

Manages real-time CloudFront log ingestion into Rawtree. Creates a Kinesis Data Stream, Kinesis Data Firehose delivery stream with HTTP endpoint destination, and a CloudFront real-time log configuration to stream CloudFront access logs to a Rawtree table, with S3 backup for failed deliveries.

## How It Works

1. A **Kinesis Data Stream** (On-Demand) receives real-time log records from CloudFront edge locations.
2. A **Kinesis Data Firehose** delivery stream reads from the Kinesis stream, buffers records, and delivers them to the Rawtree HTTP endpoint (`?transform=firehose`).
3. A **CloudFront real-time log configuration** specifies which fields to log, the sampling rate, and the target Kinesis stream.
4. The real-time log config is attached to the CloudFront distribution's default cache behavior.

Failed deliveries are backed up to an S3 bucket with a 30-day lifecycle policy.

## Example Usage

### Basic usage

```hcl
resource "rawtree_cloudfront_ingestion" "logs" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"
}
```

### With custom sampling rate and fields

```hcl
resource "rawtree_cloudfront_ingestion" "logs" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"

  sampling_rate = 50

  fields = [
    "timestamp", "c-ip", "sc-status", "sc-bytes",
    "cs-method", "cs-uri-stem", "x-edge-location",
    "x-edge-result-type", "time-taken",
  ]
}
```

### With custom buffering and organization/project

```hcl
resource "rawtree_cloudfront_ingestion" "logs" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"
  organization    = "my-org"
  project         = "my-project"

  buffering_size     = 10
  buffering_interval = 120
  s3_backup_mode     = "AllData"
}
```

## Schema

### Required

- `table` (String) - The Rawtree table name to ingest CloudFront logs into. Will be auto-created on first insert.
- `distribution_id` (String) - The ID of the CloudFront distribution to attach real-time logging to. Changing this forces a new resource.
- `region` (String) - AWS region where the Kinesis Data Stream, Firehose delivery stream, and backup bucket will be created. Changing this forces a new resource.

### Optional

- `sampling_rate` (Number) - Percentage of requests to log. Valid range: 1-100. Default: `100`.
- `fields` (Set of String) - CloudFront real-time log fields to include. Defaults to all ~40 available fields.
- `buffering_size` (Number) - Firehose buffer size in MB before delivery. Valid range: 1-64. Default: `5`.
- `buffering_interval` (Number) - Firehose buffer interval in seconds before delivery. Valid range: 60-900. Default: `300`.
- `s3_backup_mode` (String) - S3 backup mode. Valid values: `FailedDataOnly`, `AllData`. Default: `FailedDataOnly`.
- `organization` (String) - The Rawtree organization. Defaults to the provider-level organization.
- `project` (String) - The Rawtree project. Defaults to the provider-level project.

### Read-Only

- `id` (String) - The unique identifier for this ingestion resource.
- `api_url` (String) - The Rawtree API URL (from provider config).
- `api_key_hash` (String, Sensitive) - Hash of the API key. Changes trigger Firehose destination update.
- `endpoint_url` (String) - The full Firehose HTTP endpoint URL.
- `kinesis_stream_arn` (String) - The ARN of the Kinesis Data Stream.
- `kinesis_stream_name` (String) - The name of the Kinesis Data Stream.
- `firehose_arn` (String) - The ARN of the Kinesis Data Firehose delivery stream.
- `firehose_name` (String) - The name of the Kinesis Data Firehose delivery stream.
- `backup_bucket_name` (String) - The name of the S3 bucket used for failed delivery backup.
- `realtime_log_config_arn` (String) - The ARN of the CloudFront real-time log configuration.

## AWS Resources Created

| Resource | Name Pattern | Purpose |
|----------|-------------|---------|
| S3 Bucket | `rawtree-cf-backup-{name}` | Failed delivery backup (30-day lifecycle) |
| Kinesis Data Stream | `rawtree-cf-{name}` | Receives logs from CloudFront (On-Demand mode) |
| IAM Role (CloudFront) | `rawtree-cf-source-{name}` | Allows CloudFront to write to Kinesis |
| IAM Role (Firehose) | `rawtree-cf-firehose-{name}` | Allows Firehose to read Kinesis, write S3 and CloudWatch |
| Firehose Delivery Stream | `rawtree-cf-{name}` | Buffers and delivers logs to Rawtree HTTP endpoint |
| Real-Time Log Config | `rawtree-cf-{name}` | CloudFront logging configuration |

## Important Notes

- **Distribution attachment**: The resource modifies the distribution's default cache behavior to attach the real-time log config. If you manage the distribution with `aws_cloudfront_distribution`, add `lifecycle { ignore_changes = [default_cache_behavior[0].realtime_log_config_arn] }` to avoid conflicts.
- **Resource names**: Names are auto-generated from org, project, and table with a hash suffix. The `{name}` portion is max 40 characters.
- **Kinesis stream mode**: The Kinesis Data Stream is created in On-Demand mode (auto-scales, no shard management required).
- **CloudFront region**: The Kinesis stream and Firehose can be in any region, but `us-east-1` is recommended for lowest latency with CloudFront.
- **Sampling**: Use `sampling_rate` (1-100) to control what percentage of requests generate log records.
- **Import**: This resource does not support import. Create it using Terraform.
