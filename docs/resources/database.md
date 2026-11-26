---
page_title: "rawtree_database Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree database.
---

# rawtree_database (Resource)

Manages a RawTree database in the provider cluster. Every attribute forces a new database: changing one deletes the database and its tables, then creates a new one.

Create fails if a database with the same name already exists. Import it instead.

## Example Usage

```hcl
resource "rawtree_database" "analytics" {
  name = "analytics"
}
```

### With Customer-Owned S3 Storage

```hcl
resource "rawtree_database" "byob" {
  name = "byob"

  s3_storage = {
    data_bucket    = "my-rawtree-data"
    data_path      = "rawtree/"
    backups_bucket = "my-rawtree-backups"
    role_arn       = "arn:aws:iam::123456789012:role/rawtree-storage"
  }
}
```

## Schema

### Required

- `name` (String) - Database name. Changing this forces a new resource.

### Optional

- `s3_storage` (Attributes) - Customer-owned S3 storage. Tables in the database inherit it. Omit to use the cluster default storage. Changing this forces a new resource.
  - `data_bucket` (String, Required) - S3 bucket for table data.
  - `data_path` (String) - Path prefix in the data bucket.
  - `backups_bucket` (String, Required) - S3 bucket for backups.
  - `backups_path` (String) - Path prefix in the backups bucket.
  - `role_arn` (String, Required) - IAM role ARN that RawTree assumes to access the buckets.

### Read-Only

- `id` (String) - Database name.

## Import

Import a database by name:

```shell
terraform import rawtree_database.analytics analytics
```

The API does not return `s3_storage.role_arn`, so it is empty after import. Setting it in the configuration then updates the state without replacing the database.
