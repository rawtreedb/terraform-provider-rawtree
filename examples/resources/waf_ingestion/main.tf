terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

provider "rawtree" {
  # Uses the RAWTREE_API_KEY and RAWTREE_DATABASE env vars
  # or rtree CLI config from ~/.config/rtree/config.json
}

# Stream WAF logs from a CloudFront Web ACL to Rawtree in real-time.
# Creates a Kinesis Data Firehose delivery stream with HTTP endpoint
# destination and configures WAF logging.
resource "rawtree_waf_ingestion" "waf_logs" {
  table       = "waf_logs"
  web_acl_arn = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/abc123"
  region      = "us-east-1"
}

# With an explicit database and custom buffering.
resource "rawtree_waf_ingestion" "waf_logs_full_backup" {
  table              = "waf_logs_audit"
  web_acl_arn        = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-audit-acl/def456"
  region             = "us-east-1"
  database           = "security"
  buffering_size     = 10
  buffering_interval = 120
  s3_backup_mode     = "AllData"
}

output "firehose_arn" {
  value = rawtree_waf_ingestion.waf_logs.firehose_arn
}

output "backup_bucket" {
  value = rawtree_waf_ingestion.waf_logs.backup_bucket_name
}
