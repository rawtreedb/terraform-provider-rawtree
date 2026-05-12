---
page_title: "rawtree_waf_ingestion Resource - Rawtree"
subcategory: ""
description: |-
  Manages real-time AWS WAF log ingestion into Rawtree via Kinesis Data Firehose.
---

# rawtree_waf_ingestion (Resource)

Manages real-time AWS WAF log ingestion into Rawtree. Creates a Kinesis Data Firehose delivery stream with an HTTP endpoint destination to stream WAF logs from a Web ACL to Rawtree, with S3 backup for failed deliveries.

## How It Works

1. **Firehose delivery stream**: A Kinesis Data Firehose delivery stream is created with Direct PUT source and HTTP endpoint destination pointing to the Rawtree API.

2. **WAF logging configuration**: The WAFv2 Web ACL is configured to send real-time logs to the Firehose delivery stream.

3. **S3 backup**: Failed deliveries are automatically backed up to an S3 bucket with a 30-day lifecycle policy.

4. **Authentication**: The Rawtree API key is passed via the `X-Amz-Firehose-Access-Key` header (Firehose does not support `Authorization: Bearer`). The Rawtree API endpoint uses `?transform=firehose` to handle the Firehose protocol format.

## Example Usage

```hcl
resource "rawtree_waf_ingestion" "waf_logs" {
  table       = "waf_logs"
  web_acl_arn = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/abc123"
  region      = "us-east-1"
}
```

### With Explicit Organization and Project

```hcl
resource "rawtree_waf_ingestion" "waf_logs" {
  table        = "waf_logs"
  web_acl_arn  = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/abc123"
  region       = "us-east-1"
  organization = "my-org"
  project      = "my-project"
}
```

### With Custom Buffering

```hcl
resource "rawtree_waf_ingestion" "waf_logs" {
  table              = "waf_logs"
  web_acl_arn        = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/abc123"
  region             = "us-east-1"
  buffering_size     = 10
  buffering_interval = 120
  s3_backup_mode     = "AllData"
}
```

## Schema

### Required

- `table` (String) - The Rawtree table name to ingest WAF logs into. Will be auto-created on first insert.
- `web_acl_arn` (String) - The ARN of the WAFv2 Web ACL to attach logging to. Changing this forces a new resource. For CloudFront Web ACLs, the region must be `us-east-1`.
- `region` (String) - AWS region where the Firehose delivery stream and backup bucket will be created. Must match the Web ACL region (`us-east-1` for CloudFront). Changing this forces a new resource.

### Optional

- `organization` (String) - The Rawtree organization. Defaults to the provider-level organization.
- `project` (String) - The Rawtree project. Defaults to the provider-level project.
- `buffering_size` (Number) - Firehose buffer size in MB before delivery. Valid range: 1-64. Default: `5`.
- `buffering_interval` (Number) - Firehose buffer interval in seconds before delivery. Valid range: 60-900. Default: `300`.
- `s3_backup_mode` (String) - S3 backup mode. One of: `FailedDataOnly`, `AllData`. Default: `FailedDataOnly`.

### Read-Only

- `id` (String) - The unique identifier for this ingestion resource.
- `api_url` (String) - The Rawtree API base URL (from provider config).
- `endpoint_url` (String) - The full Firehose HTTP endpoint URL (e.g. `{api_url}/v1/{org}/{project}/tables/{table}?transform=firehose`). Reflects the actual URL configured on the Firehose delivery stream in AWS.
- `api_key_hash` (String, Sensitive) - Hash of the API key (from provider config).
- `firehose_arn` (String) - The ARN of the Kinesis Data Firehose delivery stream.
- `firehose_name` (String) - The name of the Kinesis Data Firehose delivery stream.
- `backup_bucket_name` (String) - The name of the S3 bucket used for failed delivery backup.
- `waf_logging_configuration_id` (String) - The ID of the WAF logging configuration.

## AWS Resources Created

| Resource | Name Pattern | Purpose |
|----------|-------------|---------|
| S3 Bucket | `rawtree-waf-backup-{name}` | Backup for failed Firehose deliveries (30-day lifecycle) |
| IAM Role | `rawtree-firehose-{name}` | Firehose execution role with S3 and CloudWatch Logs access |
| Firehose | `aws-waf-logs-rawtree-{name}` | Delivery stream from WAF to Rawtree HTTP endpoint |
| WAF Logging Config | (linked to Web ACL) | Connects WAF logs to the Firehose stream |

All resources are tagged with `managed-by: terraform-provider-rawtree`.

## S3 Backup and Failed Deliveries

The S3 backup bucket is mandatory -- Firehose requires an S3 destination for every HTTP endpoint delivery stream. When Firehose cannot deliver records to the Rawtree API (after exhausting retries with exponential backoff), it writes the failed WAF log records to the backup bucket under `firehose-failures/YYYY/MM/DD/HH/`.

- **`FailedDataOnly` (default)**: Only records that failed delivery after all retries are written to S3. Use this to minimize storage costs while keeping a safety net.
- **`AllData`**: Every record is backed up to S3 regardless of delivery success. Use this for audit or compliance requirements.

The backup bucket has a 30-day lifecycle policy so failed records don't accumulate indefinitely.

**Retrying failed records**: There is no built-in Firehose mechanism to replay records from S3. If records land in the backup bucket, you can re-ingest them by pointing a `rawtree_s3_ingestion` resource at the backup bucket, or by manually POSTing them to the Rawtree API. In practice, if delivery is failing, fix the root cause (API outage, auth issue) -- Firehose automatically retries live traffic once the endpoint recovers. The S3 backup captures records lost during the outage window.

## Important Notes

- The Firehose delivery stream name must start with `aws-waf-logs-` (AWS requirement for WAF logging).
- For CloudFront WAF, all resources must be in `us-east-1`.
- The Firehose uses Direct PUT source -- it does not use a Kinesis stream.
- One WAF Web ACL can only have one logging configuration at a time.

## Import

Import is not supported. Please create the resource using Terraform.
