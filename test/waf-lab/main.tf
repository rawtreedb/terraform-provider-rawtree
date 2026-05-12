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

variable "name_prefix" {
  type    = string
  default = "rawtree-waf-lab"
}

provider "aws" {
  region = var.region
}

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
    "managed-by" = "rawtree-waf-lab"
  }
}

resource "aws_s3_object" "index" {
  bucket       = aws_s3_bucket.origin.id
  key          = "index.html"
  content      = <<-HTML
    <!DOCTYPE html>
    <html><head><title>WAF Lab</title></head>
    <body><h1>Rawtree WAF Lab</h1><p>This origin serves traffic for WAF log generation.</p></body>
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

# ---------------------------------------------------------------------------
# CloudFront OAC + Distribution
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
    "managed-by" = "rawtree-waf-lab"
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

  # --- Custom rate-limiting rule: 100 requests per 5 min per IP ---
  rule {
    name     = "rate-limit-per-ip"
    priority = 80

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 100
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
    "managed-by" = "rawtree-waf-lab"
  }
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "cloudfront_domain" {
  value       = aws_cloudfront_distribution.main.domain_name
  description = "CloudFront domain name. Use as target for GoTestWAF and traffic scripts."
}

output "cloudfront_url" {
  value       = "https://${aws_cloudfront_distribution.main.domain_name}"
  description = "Full CloudFront URL."
}

output "web_acl_arn" {
  value       = aws_wafv2_web_acl.main.arn
  description = "WAF Web ACL ARN. Use with rawtree_waf_ingestion resource."
}

output "web_acl_name" {
  value       = aws_wafv2_web_acl.main.name
  description = "WAF Web ACL name."
}

output "origin_bucket" {
  value = aws_s3_bucket.origin.id
}

output "run_gotestwaf" {
  value       = "docker run --rm wallarm/gotestwaf --url https://${aws_cloudfront_distribution.main.domain_name} --skipWAFBlockCheck"
  description = "Run this command to generate malicious traffic."
}

output "run_legitimate_traffic" {
  value       = <<-EOT
    # Legitimate traffic (run in a loop):
    for i in $(seq 1 50); do
      curl -s -o /dev/null -w "%%{http_code} " "https://${aws_cloudfront_distribution.main.domain_name}/"
      curl -s -o /dev/null -w "%%{http_code} " "https://${aws_cloudfront_distribution.main.domain_name}/api/health"
      curl -s -o /dev/null -w "%%{http_code} " "https://${aws_cloudfront_distribution.main.domain_name}/api/users"
      curl -s -o /dev/null -w "%%{http_code} " "https://${aws_cloudfront_distribution.main.domain_name}/api/users?page=1&limit=10"
      sleep 0.5
    done
  EOT
  description = "Run this to generate legitimate traffic alongside GoTestWAF."
}
