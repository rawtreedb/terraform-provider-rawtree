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

# Basic usage - all fields, 100% sampling
resource "rawtree_cloudfront_ingestion" "basic" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"
}

# Full configuration
resource "rawtree_cloudfront_ingestion" "full" {
  table           = "cloudfront_logs"
  distribution_id = "E1234567890ABC"
  region          = "us-east-1"
  database        = "security"

  sampling_rate      = 50
  buffering_size     = 10
  buffering_interval = 120
  s3_backup_mode     = "AllData"

  fields = [
    "timestamp", "c-ip", "sc-status", "sc-bytes",
    "cs-method", "cs-uri-stem", "x-edge-location",
    "x-edge-result-type", "time-taken",
  ]
}

# Outputs
output "realtime_log_config_arn" {
  value = rawtree_cloudfront_ingestion.basic.realtime_log_config_arn
}

output "kinesis_stream_arn" {
  value = rawtree_cloudfront_ingestion.basic.kinesis_stream_arn
}
