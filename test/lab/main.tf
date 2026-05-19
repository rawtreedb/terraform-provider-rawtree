terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
    rawtree = {
      source = "rawtreedb/rawtree"
    }
  }
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "name_prefix" {
  type    = string
  default = "rawtree-lab"
}

variable "waf_table" {
  type        = string
  default     = "waf_logs"
  description = "Rawtree table name for WAF logs."
}

variable "cloudfront_table" {
  type        = string
  default     = "cloudfront_logs"
  description = "Rawtree table name for CloudFront real-time logs."
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

provider "rawtree" {}

resource "random_id" "suffix" {
  byte_length = 4
}

locals {
  name = "${var.name_prefix}-${random_id.suffix.hex}"
}

# ---------------------------------------------------------------------------
# S3 origin bucket with a simple index page
# ---------------------------------------------------------------------------

resource "aws_s3_bucket" "origin" {
  bucket        = "${local.name}-origin"
  force_destroy = true

  tags = {
    "managed-by" = "rawtree-lab"
  }
}

resource "aws_s3_object" "index" {
  bucket       = aws_s3_bucket.origin.id
  key          = "index.html"
  content      = <<-HTML
    <!DOCTYPE html>
    <html><head><title>Rawtree Lab</title></head>
    <body><h1>Rawtree Ingestion Lab</h1><p>This origin serves traffic for WAF + CloudFront log generation.</p></body>
    </html>
  HTML
  content_type = "text/html"
}

resource "aws_s3_object" "api_health" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/health"
  content      = "{\"status\":\"ok\"}"
  content_type = "application/json"
}

resource "aws_s3_object" "api_users" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/users"
  content      = "[{\"id\":1,\"name\":\"alice\"},{\"id\":2,\"name\":\"bob\"}]"
  content_type = "application/json"
}

resource "aws_s3_object" "api_login" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/login"
  content      = "{\"status\":\"ok\",\"token\":\"mock-jwt-token\"}"
  content_type = "application/json"
}

resource "aws_s3_object" "api_search" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/search"
  content      = "{\"results\":[],\"total\":0}"
  content_type = "application/json"
}

resource "aws_s3_object" "api_orders" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/orders"
  content      = "[{\"id\":1001,\"status\":\"shipped\"},{\"id\":1002,\"status\":\"pending\"}]"
  content_type = "application/json"
}

resource "aws_s3_object" "api_admin" {
  bucket       = aws_s3_bucket.origin.id
  key          = "admin/dashboard"
  content      = "{\"message\":\"admin panel\"}"
  content_type = "application/json"
}

resource "aws_s3_object" "api_upload" {
  bucket       = aws_s3_bucket.origin.id
  key          = "api/upload"
  content      = "{\"status\":\"ok\"}"
  content_type = "application/json"
}

# ---------------------------------------------------------------------------
# CloudFront OAC + Distribution (WAF + real-time logs)
# ---------------------------------------------------------------------------

resource "aws_cloudfront_origin_access_control" "main" {
  name                              = local.name
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_s3_bucket_policy" "origin" {
  bucket = aws_s3_bucket.origin.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowCloudFrontOAC"
        Effect    = "Allow"
        Principal = { Service = "cloudfront.amazonaws.com" }
        Action    = "s3:GetObject"
        Resource  = "${aws_s3_bucket.origin.arn}/*"
        Condition = {
          StringEquals = {
            "AWS:SourceArn" = aws_cloudfront_distribution.main.arn
          }
        }
      }
    ]
  })
}

resource "aws_cloudfront_distribution" "main" {
  enabled             = true
  is_ipv6_enabled     = true
  default_root_object = "index.html"
  web_acl_id          = aws_wafv2_web_acl.main.arn
  wait_for_deployment = true

  origin {
    domain_name              = aws_s3_bucket.origin.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.main.id
    origin_id                = "s3"
  }

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "s3"
    viewer_protocol_policy = "allow-all"

    forwarded_values {
      query_string = true
      headers      = ["Origin", "Access-Control-Request-Headers", "Access-Control-Request-Method"]
      cookies {
        forward = "none"
      }
    }

    min_ttl     = 0
    default_ttl = 60
    max_ttl     = 300
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
    "managed-by" = "rawtree-lab"
  }

  lifecycle {
    ignore_changes = [default_cache_behavior[0].realtime_log_config_arn]
  }
}

# ---------------------------------------------------------------------------
# WAFv2 Web ACL with production-like rules
# ---------------------------------------------------------------------------

resource "aws_wafv2_web_acl" "main" {
  name        = local.name
  scope       = "CLOUDFRONT"
  description = "Production-like WAF for log generation lab"

  default_action {
    allow {}
  }

  # --- AWS Managed Rules: Core Rule Set (SQLi, XSS, path traversal, etc.) ---
  rule {
    name     = "aws-common"
    priority = 10

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-common"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: Known Bad Inputs (Log4j, Java deserialization, etc.) ---
  rule {
    name     = "aws-known-bad-inputs"
    priority = 20

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-known-bad"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: SQL Injection ---
  rule {
    name     = "aws-sqli"
    priority = 30

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesSQLiRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-sqli"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: Linux OS (path traversal, LFI, etc.) ---
  rule {
    name     = "aws-linux"
    priority = 40

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesLinuxRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-linux"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: Amazon IP Reputation List ---
  rule {
    name     = "aws-ip-reputation"
    priority = 50

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-ip-reputation"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: Anonymous IP List (Tor, VPNs, proxies) ---
  rule {
    name     = "aws-anonymous-ip"
    priority = 60

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAnonymousIpList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-anonymous-ip"
      sampled_requests_enabled   = true
    }
  }

  # --- AWS Managed Rules: Bot Control ---
  rule {
    name     = "aws-bot-control"
    priority = 70

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesBotControlRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-bot-control"
      sampled_requests_enabled   = true
    }
  }

  # --- Custom rate-limiting rule: 10000 requests per 5 min per IP ---
  # Set high for lab use; traffic scripts send hundreds of requests per session.
  rule {
    name     = "rate-limit-per-ip"
    priority = 80

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 10000
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  # --- Custom geo-blocking rule: block specific countries ---
  rule {
    name     = "geo-block"
    priority = 90

    action {
      block {}
    }

    statement {
      geo_match_statement {
        country_codes = ["KP", "IR", "CU", "SY"]
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-geo-block"
      sampled_requests_enabled   = true
    }
  }

  # --- Custom rule: block requests with suspicious user-agents ---
  rule {
    name     = "block-suspicious-ua"
    priority = 100

    action {
      block {}
    }

    statement {
      or_statement {
        statement {
          byte_match_statement {
            search_string         = "nikto"
            positional_constraint = "CONTAINS"

            field_to_match {
              single_header {
                name = "user-agent"
              }
            }

            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
        statement {
          byte_match_statement {
            search_string         = "sqlmap"
            positional_constraint = "CONTAINS"

            field_to_match {
              single_header {
                name = "user-agent"
              }
            }

            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
        statement {
          byte_match_statement {
            search_string         = "nmap"
            positional_constraint = "CONTAINS"

            field_to_match {
              single_header {
                name = "user-agent"
              }
            }

            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
        statement {
          byte_match_statement {
            search_string         = "dirbuster"
            positional_constraint = "CONTAINS"

            field_to_match {
              single_header {
                name = "user-agent"
              }
            }

            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-suspicious-ua"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = local.name
    sampled_requests_enabled   = true
  }

  tags = {
    "managed-by" = "rawtree-lab"
  }
}

# ---------------------------------------------------------------------------
# Rawtree WAF Ingestion (WAF logs -> Firehose -> Rawtree)
# ---------------------------------------------------------------------------

resource "rawtree_waf_ingestion" "main" {
  table       = var.waf_table
  web_acl_arn = aws_wafv2_web_acl.main.arn
  region      = var.region
}

# ---------------------------------------------------------------------------
# Rawtree CloudFront Ingestion (CF real-time logs -> Kinesis -> Firehose -> Rawtree)
# ---------------------------------------------------------------------------

resource "rawtree_cloudfront_ingestion" "main" {
  table           = var.cloudfront_table
  distribution_id = aws_cloudfront_distribution.main.id
  region          = var.region
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "cloudfront_domain" {
  value       = aws_cloudfront_distribution.main.domain_name
  description = "CloudFront domain name. Target for traffic generation."
}

output "cloudfront_url" {
  value       = "https://${aws_cloudfront_distribution.main.domain_name}"
  description = "Full CloudFront URL."
}

output "distribution_id" {
  value = aws_cloudfront_distribution.main.id
}

output "web_acl_arn" {
  value = aws_wafv2_web_acl.main.arn
}

output "waf_firehose_name" {
  value       = rawtree_waf_ingestion.main.firehose_name
  description = "WAF Firehose delivery stream name."
}

output "waf_firehose_arn" {
  value       = rawtree_waf_ingestion.main.firehose_arn
  description = "WAF Firehose delivery stream ARN."
}

output "cf_kinesis_stream_name" {
  value       = rawtree_cloudfront_ingestion.main.kinesis_stream_name
  description = "CloudFront Kinesis data stream name."
}

output "cf_firehose_name" {
  value       = rawtree_cloudfront_ingestion.main.firehose_name
  description = "CloudFront Firehose delivery stream name."
}

output "cf_realtime_log_config_arn" {
  value       = rawtree_cloudfront_ingestion.main.realtime_log_config_arn
  description = "CloudFront real-time log config ARN."
}

output "origin_bucket" {
  value = aws_s3_bucket.origin.id
}

output "run_gotestwaf" {
  value       = "docker run --rm wallarm/gotestwaf --url https://${aws_cloudfront_distribution.main.domain_name} --skipWAFBlockCheck"
  description = "Run this command to generate malicious traffic (produces both WAF + CF logs)."
}

output "run_traffic" {
  value       = "./generate-traffic.sh ${aws_cloudfront_distribution.main.domain_name}"
  description = "Run the traffic generator (produces both WAF + CF real-time logs)."
}
