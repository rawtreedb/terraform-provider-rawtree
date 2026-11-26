---
page_title: "RawTree Provider"
subcategory: ""
description: |-
  Manage RawTree databases, tables and API keys, and ingest AWS data into RawTree.
---

# RawTree Provider

Use the RawTree provider to manage [RawTree](https://rawtree.com) resources and to build AWS pipelines that ingest data into RawTree.

## Credentials

The provider uses a RawTree API key (`rt_...`). The key selects its organization, cluster and default database.

The provider resolves each value in this order:

1. The provider block
2. Environment variables: `RAWTREE_API_KEY`, `RAWTREE_API_URL`, `RAWTREE_DATABASE`
3. The rtree CLI config file (`~/.config/rtree/config.json`): `token`, `url`, `database`

## Example Usage

```hcl
terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

# Data-plane resources and the AWS ingestion resources use an API key.
# The key selects its organization, cluster and default database.
# Environment variables: RAWTREE_API_KEY, RAWTREE_API_URL, RAWTREE_DATABASE.
# If you logged in with the rtree CLI, the provider reads its config file.
provider "rawtree" {
  api_key  = var.rawtree_api_key
  database = "analytics" # optional, defaults to the API key's database
}

variable "rawtree_api_key" {
  type      = string
  sensitive = true
}
```

## Schema

### Optional

- `api_key` (String, Sensitive) - RawTree API key (`rt_...`). Required by the AWS ingestion resources. Can also be set via `RAWTREE_API_KEY`.
- `api_url` (String) - RawTree API URL. Defaults to `https://api.rawtree.com`. Can also be set via `RAWTREE_API_URL`.
- `organization` (String) - Organization name. Only `rawtree_supabase_cdc_ingestion` reads this value. Can also be set via `RAWTREE_ORG`.
- `database` (String) - Default database for tables and ingestion. Defaults to the API key's database. Can also be set via `RAWTREE_DATABASE`.
- `project` (String, Deprecated) - RawTree no longer has projects. Only `rawtree_supabase_cdc_ingestion` reads this value. Can also be set via `RAWTREE_PROJECT`.

## Upgrade from 0.1

- Remove `project` from the provider block and from the ingestion resources. Remove `organization` from the ingestion resources. Set `database` if you do not use the API key's database.
- Replace `RAWTREE_URL` with `RAWTREE_API_URL`. The old name still works.
- The default API URL is now `https://api.rawtree.com`.
- The first `terraform apply` after the upgrade updates each WAF, CloudFront and S3 ingestion pipeline to the new ingest URL (`/v1/tables/{table}`). It does not replace AWS resources.
