terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}

provider "rawtree" {
  # Uses RAWTREE_API_KEY, RAWTREE_ORG, RAWTREE_PROJECT env vars
  # or rtree CLI config from ~/.config/rtree/config.json
}

# Ingest all JSON files from an S3 bucket into a Rawtree table.
# Existing files are ingested via a one-time Glue job.
# New files are automatically ingested via EventBridge + Lambda.
resource "rawtree_s3_ingestion" "events" {
  table        = "events"
  bucket       = "my-data-bucket"
  prefix       = "data/events/"
  file_pattern = ".*\\.json$"
  format       = "json"
  region       = "us-east-1"
}

# Ingest Parquet files from a different prefix.
resource "rawtree_s3_ingestion" "analytics" {
  table  = "analytics"
  bucket = "my-data-bucket"
  prefix = "data/analytics/"
  format = "parquet"
  region = "us-east-1"
}

output "events_glue_job" {
  value = rawtree_s3_ingestion.events.glue_job_name
}

output "events_lambda_arn" {
  value = rawtree_s3_ingestion.events.lambda_function_arn
}
