---
page_title: "rawtree_workflow Resource - Tinybird RawTree"
subcategory: "Tinybird RawTree"
description: Manage a SQL workflow in an existing Tinybird RawTree cluster.
---

# rawtree_workflow

Manage the SQL definition and optional UTC schedule for a Tinybird RawTree workflow. The cluster must have workflows enabled. Use an organization administrator's session or a cluster-scoped admin API key to create, update, or delete workflows.

## Example

```hcl
resource "rawtree_workflow" "daily_events" {
  organization = "acme"
  cluster      = "production"
  name         = "daily-events"

  query = {
    database = "analytics"
    sql      = "SELECT count() AS events FROM events"
  }

  schedule = {
    cron    = "0 8 * * *"
    enabled = false
  }
}
```

## Arguments

- `organization` and `cluster` (required) select an existing cluster. Changing either replaces the workflow.
- `name` (required) must be unique in the cluster. The API stores names in lowercase. Renaming a workflow keeps its UUID.
- `description` (optional) accepts up to 1,000 characters. Remove it to clear the description.
- `query` (required) contains the `database` name and a `sql` string. Workflows support read queries and `INSERT INTO ... SELECT`.
- `schedule` (optional) contains a five- or six-field UTC `cron` expression. Its `enabled` flag defaults to true. Set it to false to pause the schedule. Omit the schedule to run the workflow manually.

Terraform exposes the API's UUID as `workflow_id` and the full import identity as `id`. Destroying this resource deletes the workflow and its schedule. Applying a manual-only definition does not execute its SQL.

## Result destinations

Use the optional ordered `sinks` list to manage up to five destinations. Set one destination per entry:

```hcl
sinks = [
  { table = { database = "analytics", table = "daily_event_counts" } },
  { http = { url = var.webhook_url, headers = var.webhook_headers } },
  { slack = { integration_id = var.slack_integration_id, channel_id = "C0123456789" } },
]
```

Reference a connected Slack integration and its stable channel ID. Terraform retains sink IDs when you reorder destinations. Changing the target creates a new sink. Removing a sink from the list removes it from the workflow. Removing the entire list clears all sinks.

The provider marks the HTTP URL and headers as sensitive. Terraform state can contain configured values. Tinybird RawTree returns header names only. Omitting `headers` preserves existing headers on the same destination. Use `headers = {}` to clear them. Terraform cannot detect changes to a header value made outside Terraform.

## Import

Use the workflow UUID shown in the Tinybird RawTree workflow URL or returned by the API.

```shell
terraform import rawtree_workflow.daily_events acme/production/11111111-1111-4111-8111-111111111111
```

To generate configuration for existing workflows, use the [workflow list resource](../list-resources/workflow.md).
