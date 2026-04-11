terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "bucket_suffix" {
  type        = string
  default     = ""
  description = "Optional suffix for bucket name uniqueness. If empty, a random one is generated."
}

provider "aws" {
  region = var.region
}

resource "random_id" "suffix" {
  byte_length = 4
}

locals {
  suffix      = var.bucket_suffix != "" ? var.bucket_suffix : random_id.suffix.hex
  bucket_name = "rawtree-provider-test-${local.suffix}"
}

# Test bucket
resource "aws_s3_bucket" "test" {
  bucket        = local.bucket_name
  force_destroy = true

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "acceptance-testing"
  }
}

# Enable EventBridge notifications (required for the provider)
resource "aws_s3_bucket_notification" "test" {
  bucket      = aws_s3_bucket.test.id
  eventbridge = true
}

# Upload JSON test data
resource "aws_s3_object" "events_json" {
  bucket       = aws_s3_bucket.test.id
  key          = "data/json/events.json"
  source       = "${path.module}/../data/events.json"
  content_type = "application/json"
  etag         = filemd5("${path.module}/../data/events.json")
}

# Upload CSV test data
resource "aws_s3_object" "metrics_csv" {
  bucket       = aws_s3_bucket.test.id
  key          = "data/csv/metrics.csv"
  source       = "${path.module}/../data/metrics.csv"
  content_type = "text/csv"
  etag         = filemd5("${path.module}/../data/metrics.csv")
}

# Upload JSONL test data
resource "aws_s3_object" "logs_jsonl" {
  bucket       = aws_s3_bucket.test.id
  key          = "data/json/logs.jsonl"
  source       = "${path.module}/../data/logs.jsonl"
  content_type = "application/x-ndjson"
  etag         = filemd5("${path.module}/../data/logs.jsonl")
}

# Outputs for use in acceptance tests
output "bucket_name" {
  value       = aws_s3_bucket.test.id
  description = "Set this as RAWTREE_TEST_S3_BUCKET when running acceptance tests."
}

output "region" {
  value = var.region
}

output "test_env" {
  value       = <<-EOT
    export RAWTREE_TEST_S3_BUCKET="${aws_s3_bucket.test.id}"
    export AWS_REGION="${var.region}"
  EOT
  description = "Paste this into your shell before running acceptance tests."
}
