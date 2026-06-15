# Self-contained example: dual-stack VPC + Supabase CDC ingestion resource.
#
# The Fargate worker needs both:
#   - IPv6 egress to Supabase's direct Postgres endpoint (it's IPv6-only on
#     most Supabase projects),
#   - IPv4 egress for the worker's container image pull from ghcr.io.
# This file sets both up via a single dual-stack VPC + IGW: tasks get a public
# IPv4 (cheaper than a NAT Gateway) and a routable IPv6 from the subnet's /64.
#
# Production note: replace this VPC with your own. To avoid per-task public
# IPv4s, switch to private subnets + a NAT Gateway for IPv4 egress + an
# egress-only IGW for IPv6 — and drop assign_public_ip = true below.
#
# Usage:
#   terraform init
#   export TF_VAR_supabase_database_url='postgres://postgres:PASS@db.<ref>.supabase.co:5432/postgres?sslmode=require'
#   export TF_VAR_supabase_tls_root_cert_path="$HOME/supabase-ca.crt"
#   export TF_VAR_publication='rawtree_orders_publication'
#   terraform apply

terraform {
  required_providers {
    rawtree = {
      source = "rawtreedb/rawtree"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "rawtree" {
  # Uses RAWTREE_API_KEY, RAWTREE_URL, RAWTREE_ORG, RAWTREE_PROJECT env vars,
  # or the rtree CLI config at ~/.config/rtree/config.json.
}

provider "aws" {
  region = var.region
}

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "name" {
  type        = string
  default     = "orders"
  description = "Logical name for this CDC worker. Used as a suffix on AWS resource names."
}

variable "publication" {
  type        = string
  description = "Name of the Postgres logical-replication publication on the source database. Create it ahead of time: CREATE PUBLICATION ... FOR TABLE ..."
}

variable "supabase_database_url" {
  type        = string
  sensitive   = true
  description = "Supabase direct Postgres URL. Project Settings → Database → Connection string → Direct connection. Do NOT use the pooler URL — logical replication requires a direct connection."
}

variable "supabase_tls_root_cert_path" {
  type        = string
  description = "Path to a local file containing the Supabase project's CA PEM. Supabase signs Postgres certs with a private CA that isn't in Mozilla's root bundle, so the worker needs this explicit anchor. Download from Project Settings → Database → SSL Configuration → Download certificate."
}

# ---------------------------------------------------------------------------
# Network: dual-stack VPC + public subnet + IGW
# ---------------------------------------------------------------------------

resource "aws_vpc" "this" {
  cidr_block                       = "10.99.0.0/16"
  assign_generated_ipv6_cidr_block = true
  enable_dns_hostnames             = true
  enable_dns_support               = true

  tags = { Name = "rawtree-supabase-cdc-${var.name}" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "rawtree-supabase-cdc-${var.name}" }
}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_subnet" "this" {
  vpc_id                          = aws_vpc.this.id
  cidr_block                      = cidrsubnet(aws_vpc.this.cidr_block, 8, 0)
  ipv6_cidr_block                 = cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, 0)
  assign_ipv6_address_on_creation = true
  availability_zone               = data.aws_availability_zones.available.names[0]

  tags = { Name = "rawtree-supabase-cdc-${var.name}" }
}

resource "aws_route_table" "this" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  # IPv6 egress through the same IGW. The task ENI gets a globally routable
  # IPv6 address from the subnet's /64, so no NAT or egress-only IGW needed.
  route {
    ipv6_cidr_block = "::/0"
    gateway_id      = aws_internet_gateway.this.id
  }

  tags = { Name = "rawtree-supabase-cdc-${var.name}" }
}

resource "aws_route_table_association" "this" {
  subnet_id      = aws_subnet.this.id
  route_table_id = aws_route_table.this.id
}

# ---------------------------------------------------------------------------
# CDC ingestion
# ---------------------------------------------------------------------------

resource "rawtree_supabase_cdc_ingestion" "this" {
  name        = var.name
  region      = var.region
  publication = var.publication

  # Inline values for simplicity. For production, use
  # database_url_secret_arn / tls_root_cert_secret_arn that point at
  # Secrets Manager ARNs you manage outside this stack — those values
  # never enter Terraform state.
  database_url      = var.supabase_database_url
  tls_root_cert_pem = file(var.supabase_tls_root_cert_path)

  subnet_ids       = [aws_subnet.this.id]
  assign_public_ip = true # so the task ENI gets a public IPv4 for ghcr.io image pull

  cpu    = 512
  memory = 1024

  depends_on = [aws_route_table_association.this]
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "ecs_service_arn" {
  value = rawtree_supabase_cdc_ingestion.this.service_arn
}

output "log_group_name" {
  value       = rawtree_supabase_cdc_ingestion.this.log_group_name
  description = "Tail the worker logs with: aws logs tail $(terraform output -raw log_group_name) --follow"
}
