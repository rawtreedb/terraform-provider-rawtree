---
page_title: "Rawtree Provider"
subcategory: ""
description: |-
  The Rawtree provider enables automated data ingestion from AWS sources into Rawtree.
---

# Rawtree Provider

The Rawtree provider enables automated data ingestion from AWS sources into [Rawtree](https://rawtree.dev), a schemaless analytical database.

## Authentication

The provider resolves credentials in this order:

1. Provider configuration block
2. Environment variables (`RAWTREE_API_KEY`, `RAWTREE_URL`, `RAWTREE_ORG`, `RAWTREE_PROJECT`)
3. Rawtree CLI config file (`~/.config/rtree/config.json`)

## Example Usage

```hcl
provider "rawtree" {
  api_key      = var.rawtree_api_key
  organization = "my-org"
  project      = "my-project"
}
```

## Schema

### Optional

- `api_key` (String, Sensitive) - Rawtree API key. Can also be set via `RAWTREE_API_KEY` env var.
- `api_url` (String) - Rawtree API URL. Defaults to `https://api.us-east-1.aws.rawtree.com`.
- `organization` (String) - Rawtree organization name. Can also be set via `RAWTREE_ORG` env var.
- `project` (String) - Rawtree project name. Can also be set via `RAWTREE_PROJECT` env var.
