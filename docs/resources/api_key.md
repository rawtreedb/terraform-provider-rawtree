---
page_title: "rawtree_api_key Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree API key.
---

# rawtree_api_key (Resource)

Manages a RawTree API key in the provider cluster. API keys are immutable: changing any attribute deletes the key and creates a new one with a new token.

## Example Usage

```hcl
resource "rawtree_api_key" "ingest" {
  name       = "firehose-ingest"
  database   = "analytics"
  permission = "write_only"
  expires_at = "2027-12-31T00:00:00Z"
}

output "ingest_token" {
  value     = rawtree_api_key.ingest.token
  sensitive = true
}
```

## Schema

### Required

- `name` (String) - API key name.

### Optional

- `database` (String) - Default database stored on the key. It does not restrict the key to that database. Defaults to the provider `database`, else `default`.
- `permission` (String) - One of `admin`, `read_write`, `write_only`, `read_only`. Exactly one of `permission` and `database_roles` is required.
- `database_roles` (Set of String) - Existing ClickHouse roles the key acts with. Requires administrator access. These keys can run queries and insert into existing tables, but have no RawTree management permissions.
- `expires_at` (String) - Expiration instant in RFC 3339 format. Omit for a key that never expires.
- `principal_type` (String) - Identity the key acts as: `user` or `service_account`. Defaults to the caller. Set together with `principal_id`. With `user`, the API accepts only the ID of the caller. A call with an API key cannot select a different principal. Assigning a service account requires organization admin access.
- `principal_id` (String) - UUID of the user or service account.

### Read-Only

- `id` (String) - API key UUID.
- `token` (String, Sensitive) - The API key secret (`rt_...`).
- `created_at` (String) - Creation time.

## Import

Import an API key by UUID:

```shell
terraform import rawtree_api_key.ingest 8d6f0d3e-2f4e-4c55-9a51-0d9c2e6f1a11
```

The API returns the token only when the key is created, so `token` is null after import.
