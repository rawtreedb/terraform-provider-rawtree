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

  default_tags {
    tags = {
      CostGroup   = "e2e"
      Environment = "terraform-provider-rawtree"
    }
  }
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

# WAFv2 Web ACL for waf_ingestion acceptance tests (CloudFront scope = CLOUDFRONT, region must be us-east-1).
resource "aws_wafv2_web_acl" "test" {
  name        = "rawtree-provider-test-${local.suffix}"
  description = "Test Web ACL for terraform-provider-rawtree acceptance tests"
  scope       = "CLOUDFRONT"

  default_action {
    allow {}
  }

  visibility_config {
    cloudwatch_metrics_enabled = false
    metric_name                = "rawtree-provider-test-${local.suffix}"
    sampled_requests_enabled   = false
  }

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "acceptance-testing"
  }
}

# CloudFront distribution for cloudfront_ingestion acceptance tests.
resource "aws_s3_bucket" "cf_origin" {
  bucket        = "rawtree-cf-test-origin-${local.suffix}"
  force_destroy = true

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "acceptance-testing"
  }
}

resource "aws_cloudfront_origin_access_control" "test" {
  name                              = "rawtree-cf-test-${local.suffix}"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "test" {
  enabled             = true
  is_ipv6_enabled     = true
  wait_for_deployment = true

  origin {
    domain_name              = aws_s3_bucket.cf_origin.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.test.id
    origin_id                = "s3origin"
  }

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "s3origin"
    viewer_protocol_policy = "allow-all"

    forwarded_values {
      query_string = true
      cookies {
        forward = "none"
      }
    }

    min_ttl     = 0
    default_ttl = 60
    max_ttl     = 60
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = {
    "managed-by" = "terraform-provider-rawtree-tests"
    "purpose"    = "acceptance-testing"
  }

  lifecycle {
    ignore_changes = [default_cache_behavior[0].realtime_log_config_arn]
  }
}

# Outputs for use in acceptance tests
output "bucket_name" {
  value       = aws_s3_bucket.test.id
  description = "Set this as RAWTREE_TEST_S3_BUCKET when running acceptance tests."
}

output "region" {
  value = var.region
}

output "web_acl_arn" {
  value       = aws_wafv2_web_acl.test.arn
  description = "Set this as RAWTREE_TEST_WAF_WEB_ACL_ARN when running acceptance tests."
}

output "cf_distribution_id" {
  value       = aws_cloudfront_distribution.test.id
  description = "Set this as RAWTREE_TEST_CF_DISTRIBUTION_ID when running acceptance tests."
}

output "test_env" {
  value       = <<-EOT
    export RAWTREE_TEST_S3_BUCKET="${aws_s3_bucket.test.id}"
    export RAWTREE_TEST_WAF_WEB_ACL_ARN="${aws_wafv2_web_acl.test.arn}"
    export RAWTREE_TEST_CF_DISTRIBUTION_ID="${aws_cloudfront_distribution.test.id}"
    export AWS_REGION="${var.region}"
  EOT
  description = "Paste this into your shell before running acceptance tests."
}
