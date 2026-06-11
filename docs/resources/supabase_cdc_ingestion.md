---
page_title: "rawtree_supabase_cdc_ingestion Resource - Rawtree"
subcategory: ""
description: |-
  Manages Supabase Postgres CDC ingestion into Rawtree using ECS Fargate.
---

# rawtree_supabase_cdc_ingestion (Resource)

Manages Supabase Postgres CDC ingestion into Rawtree using a single ECS Fargate service. The worker runs the Rawtree Supabase ETL container, connects to the Supabase direct Postgres endpoint, consumes a logical replication publication, and writes CDC events to Rawtree tables.

## How It Works

1. **Secrets**: The provider creates a single managed Secrets Manager secret holding the Rawtree API key as JSON. If `database_url` and/or `tls_root_cert_pem` are passed inline they are added to the same JSON document (keys `DATABASE_URL`, `POSTGRES_TLS_ROOT_CERTS`). ECS reads each value via the JSON-key `valueFrom` syntax and injects it into the container as the env var of the same name. External `database_url_secret_arn` / `tls_root_cert_secret_arn` are referenced directly and not copied.

The supabase/etl worker reads the CA from the `POSTGRES_TLS_ROOT_CERTS` env var directly (as PEM content), so no on-disk file is needed. Supabase direct Postgres uses a private CA that isn't in Mozilla's root bundle — set `tls_root_cert_pem` (or `tls_root_cert_secret_arn`) for any non-local deployment, otherwise TLS verification will fail with `UnknownIssuer`.

2. **Runtime**: The provider creates an ECS cluster, task definition, service, execution role, and CloudWatch log group. The service runs exactly one Fargate task for the CDC worker.

3. **Initialization**: By default, Terraform runs a one-off ECS task with `initialization_command = ["init"]` before starting the service. The long-running service uses `worker_command = ["run"]`.

## Example Usage

```hcl
resource "rawtree_supabase_cdc_ingestion" "orders" {
  name        = "orders"
  region      = "us-east-1"
  publication = "rawtree_publication"

  database_url_secret_arn = aws_secretsmanager_secret.supabase_database_url.arn
  tls_root_cert_secret_arn = aws_secretsmanager_secret.supabase_ca.arn

  subnet_ids         = var.private_subnet_ids
  security_group_ids = [aws_security_group.rawtree_supabase_cdc.id]

  cpu    = 512
  memory = 1024
}
```

For quick tests, you can pass the database URL inline:

```hcl
resource "rawtree_supabase_cdc_ingestion" "orders" {
  name         = "orders"
  region       = "us-east-1"
  publication  = "rawtree_publication"
  database_url = var.supabase_database_url
  subnet_ids   = var.private_subnet_ids
}
```

The inline value is sensitive, but it is still stored in Terraform state. Use `database_url_secret_arn` for production.

## Schema

### Required

- `name` (String) - Stable name for this CDC worker. Changing this forces a new resource.
- `region` (String) - AWS region where ECS, IAM, CloudWatch Logs, and managed secrets will be created. Changing this forces a new resource.
- `publication` (String) - Postgres publication consumed by supabase/etl.
- `subnet_ids` (List of String) - Subnet IDs where the Fargate task should run. These subnets need outbound access to Supabase and Rawtree.

Exactly one of the following is required:

- `database_url` (String, Sensitive) - Supabase direct Postgres URL. The provider stores it in a managed Secrets Manager secret. Changing this forces a new resource.
- `database_url_secret_arn` (String) - ARN of an existing Secrets Manager secret containing the Supabase direct Postgres URL. Changing this forces a new resource.

### Optional

- `tls_root_cert_pem` (String, Sensitive) - Supabase database CA certificate PEM. The provider stores it in a managed Secrets Manager secret. Changing this forces a new resource.
- `tls_root_cert_secret_arn` (String) - ARN of an existing Secrets Manager secret containing the Supabase database CA certificate PEM. Changing this forces a new resource.
- `pipeline_id` (String) - supabase/etl pipeline identifier. Default: `1`.
- `image` (String) - Container image for the worker. Default: `ghcr.io/rawtreedb/supabase-etl:latest`.
- `cpu` (Number) - Fargate task CPU units. Default: `512`.
- `memory` (Number) - Fargate task memory in MiB. Default: `1024`.
- `security_group_ids` (List of String) - Security group IDs for the Fargate task. If omitted, ECS uses the VPC default security group.
- `assign_public_ip` (Boolean) - Whether the Fargate task should receive a public IPv4 address. Default: `false`.
- `log_retention_days` (Number) - CloudWatch Logs retention in days. Default: `30`.
- `run_initialization_task` (Boolean) - Run a one-off ECS task before starting the service. Default: `true`.
- `initialization_command` (List of String) - Command for the optional one-off initialization task. Default: `["init"]`.
- `worker_command` (List of String) - Command for the long-running worker container. Default: `["run"]`.
- `environment` (Map of String) - Additional non-sensitive environment variables passed to both init and worker containers.
- `organization` (String) - The Rawtree organization. Defaults to the provider-level organization.
- `project` (String) - The Rawtree project. Defaults to the provider-level project.

### Read-Only

- `id` (String) - The unique identifier for this ingestion resource.
- `api_url` (String) - The Rawtree API URL from provider config.
- `api_key_hash` (String, Sensitive) - Hash of the Rawtree API key.
- `cluster_arn` (String) - The ARN of the ECS cluster.
- `service_arn` (String) - The ARN of the ECS service.
- `task_definition_arn` (String) - The ARN of the active ECS task definition.
- `log_group_name` (String) - The CloudWatch Logs group used by the worker.
- `execution_role_arn` (String) - The IAM execution role used by ECS.
- `config_secret_arn` (String) - ARN of the managed Secrets Manager secret holding the Rawtree API key — and, when supplied inline, the Supabase database URL and CA certificate — as JSON keys (`RAWTREE_API_KEY`, `DATABASE_URL`, `POSTGRES_TLS_ROOT_CERT_PEM`).

## AWS Resources Created

| Resource | Name Pattern | Purpose |
|----------|--------------|---------|
| Secrets Manager Secret | `rawtree/supabase-cdc/{name}/config` | JSON-encoded secret holding `RAWTREE_API_KEY` (always), plus `DATABASE_URL` and/or `POSTGRES_TLS_ROOT_CERT_PEM` when those values are supplied inline. |
| CloudWatch Log Group | `/aws/ecs/rawtree/supabase-cdc/{name}` | ECS task logs |
| IAM Role | `rawtree-ecs-{name}` | ECS task execution role |
| ECS Cluster | `rawtree-supabase-cdc-{name}` | Fargate service cluster |
| ECS Task Definition | `rawtree-supabase-cdc-{name}` | Worker task definition |
| ECS Service | `rawtree-supabase-cdc-{name}` | Long-running CDC worker, desired count 1 |

All resources are tagged with `managed-by: terraform-provider-rawtree`.

## Networking

The selected subnets must have outbound access to the Supabase direct Postgres endpoint and the Rawtree API. Supabase direct Postgres can be IPv6-only, so use subnets and routing that support the endpoint your Supabase project exposes.

## Import

Import is not supported. Please create the resource using Terraform.
