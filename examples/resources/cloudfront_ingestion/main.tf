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
  organization    = "my-org"
  project         = "my-project"

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
