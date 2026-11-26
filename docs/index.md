---
page_title: "RawTree Provider"
subcategory: ""
description: |-
  Manage RawTree databases, tables, API keys, workflows, clusters, connectors and identity, and ingest AWS data into RawTree.
---

# RawTree Provider

Use the RawTree provider to manage [RawTree](https://rawtree.com) resources and to build AWS pipelines that ingest data into RawTree.

## Credentials

RawTree has two credential types. Each resource page tells which credential the resource needs.

| Credential | Format | Use it for |
|------------|--------|------------|
| API key | `rt_...` | Databases, tables, API keys, workflows, and all AWS ingestion resources. The key selects its organization, cluster and default database. |
| Access token | User JWT (OAuth access token or session token) | Clusters, connectors, service accounts, federation issuers and federation rules. API keys cannot manage these. |

You can set both. Data-plane resources use the API key when it is set, else the access token.

The access token must belong to a user who is an organization administrator. A user OAuth access token expires after 15 minutes. `rtree login` stores one as `token` in `~/.config/rtree/config.json` and the CLI refreshes it; the provider does not refresh it, so start `terraform apply` with a new token.

Workload federation tokens (from `https://api.rawtree.com/oauth/token`) are not supported: the API rejects them on the endpoints that these resources use. Thus the access-token resources are for interactive use only: run them from a workstation, not in CI. The deletion of a `rawtree_service_account` also needs a first-party session token; the API rejects an OAuth access token for it.

The provider resolves each value in this order:

1. The provider block
2. Environment variables: `RAWTREE_API_KEY`, `RAWTREE_ACCESS_TOKEN`, `RAWTREE_API_URL`, `RAWTREE_ORG`, `RAWTREE_CLUSTER`, `RAWTREE_DATABASE`
3. The rtree CLI config file (`~/.config/rtree/config.json`): `token` (the API key if it starts with `rt_`, else the access token), `url`, `default_organization`, `cluster`, `database`

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

# Clusters, connectors, service accounts and federation need an access token
# (user OAuth or session JWT) and an organization.
# Environment variables: RAWTREE_ACCESS_TOKEN, RAWTREE_ORG, RAWTREE_CLUSTER.
provider "rawtree" {
  alias        = "admin"
  access_token = var.rawtree_access_token
  organization = "my-org"
  cluster      = "production"
}

variable "rawtree_api_key" {
  type      = string
  sensitive = true
}

variable "rawtree_access_token" {
  type      = string
  sensitive = true
}
```

## Schema

### Optional

- `api_key` (String, Sensitive) - RawTree API key (`rt_...`). Required by the AWS ingestion resources. Can also be set via `RAWTREE_API_KEY`.
- `access_token` (String, Sensitive) - RawTree user access token (OAuth access token or session token; workload federation tokens are not supported). Required for clusters, connectors, service accounts and federation. Can also be set via `RAWTREE_ACCESS_TOKEN`.
- `api_url` (String) - RawTree API URL. Defaults to `https://api.rawtree.com`. Can also be set via `RAWTREE_API_URL`. Calls with an access token go to the regional host of the cluster (`https://api.<deployment>.<region>.aws.rawtree.com`).
- `organization` (String) - Organization name. Used with `access_token`. Can also be set via `RAWTREE_ORG`.
- `cluster` (String) - Cluster name or ID. Used with `access_token` for cluster-scoped resources. Can also be set via `RAWTREE_CLUSTER`.
- `database` (String) - Default database for tables, workflows and ingestion. Defaults to the API key's database. Can also be set via `RAWTREE_DATABASE`.
- `project` (String, Deprecated) - RawTree no longer has projects. Only `rawtree_supabase_cdc_ingestion` reads this value. Can also be set via `RAWTREE_PROJECT`.

## Upgrade from 0.1

- Remove `project` from the provider block and from the ingestion resources. Remove `organization` from the ingestion resources. Set `database` if you do not use the API key's database.
- Replace `RAWTREE_URL` with `RAWTREE_API_URL`. The old name still works.
- The default API URL is now `https://api.rawtree.com`.
- The first `terraform apply` after the upgrade updates each WAF, CloudFront and S3 ingestion pipeline to the new ingest URL (`/v1/tables/{table}`). It does not replace AWS resources.
