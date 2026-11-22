---
page_title: "rawtree_database Resource - Tinybird RawTree"
subcategory: "Tinybird RawTree"
description: Manage a named database in a Tinybird RawTree cluster.
---

# rawtree_database

Creates a database in an existing cluster. New databases inherit the cluster's storage settings.
Destroying or replacing this resource deletes the database and its data.

## Example

```hcl
resource "rawtree_database" "analytics" {
  organization = "acme"
  cluster      = "production"
  name         = "analytics"
}
```

## Arguments

- `organization` (required, string) is the organization name.
- `cluster` (required, string) is the cluster name within the organization.
- `name` (required, string) starts with a letter and contains up to 64 ASCII letters, digits, or underscores.

Changing any argument replaces the database. Tinybird RawTree manages the built-in databases.
The read-only `id` contains the organization, cluster, and database names.

## Authentication

Supply a cluster-scoped admin API key through `RAWTREE_MANAGEMENT_TOKEN`, or use a user credential with permission to manage databases in the selected cluster.
This resource calls the Tinybird RawTree API directly.

## Import

Import an existing database by its organization, cluster, and name.

```sh
terraform import rawtree_database.analytics acme/production/analytics
```

Give each database one Terraform owner. The Tinybird RawTree create endpoint accepts repeated requests for the same name.

To discover databases in an existing cluster and generate their configuration together, use the [database list resource](../list-resources/database.md).
