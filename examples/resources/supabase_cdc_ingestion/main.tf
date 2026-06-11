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

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "private_subnet_ids" {
  type = list(string)
}

variable "security_group_ids" {
  type    = list(string)
  default = []
}

variable "supabase_database_url_secret_arn" {
  type        = string
  description = "Secrets Manager secret ARN containing the Supabase direct Postgres URL."
}

variable "supabase_tls_root_cert_secret_arn" {
  type        = string
  default     = null
  description = "Optional Secrets Manager secret ARN containing the Supabase database CA PEM."
}

resource "rawtree_supabase_cdc_ingestion" "orders" {
  name        = "orders"
  region      = var.region
  publication = "rawtree_publication"

  database_url_secret_arn  = var.supabase_database_url_secret_arn
  tls_root_cert_secret_arn = var.supabase_tls_root_cert_secret_arn

  subnet_ids         = var.private_subnet_ids
  security_group_ids = var.security_group_ids

  cpu    = 512
  memory = 1024
}

output "ecs_service_arn" {
  value = rawtree_supabase_cdc_ingestion.orders.service_arn
}

output "log_group_name" {
  value = rawtree_supabase_cdc_ingestion.orders.log_group_name
}
