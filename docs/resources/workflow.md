---
page_title: "rawtree_workflow Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree workflow: a SQL query that runs on a schedule and delivers its rows to sinks.
---

# rawtree_workflow (Resource)

Manages a RawTree workflow. A workflow runs a SQL query on a cron schedule (or only when started manually) and delivers the rows to up to five sinks: a RawTree table, an HTTP endpoint or a Slack channel.

Sinks are managed inline. On update the provider sends the full sink list with the IDs of the existing sinks, so the API updates those sinks in place. A sink keeps its ID while the sink at the same position in the list keeps its kind (`table`, `http` or `slack`). Sinks removed from the list are deleted.

## Example Usage

```hcl
resource "rawtree_workflow" "errors" {
  name     = "errors"
  database = "analytics"
  sql      = "SELECT * FROM events WHERE level = 'error' AND ts > now() - INTERVAL 5 MINUTE"

  schedule = {
    cron = "*/5 * * * *"
  }

  sinks = [
    {
      table = {
        database = "analytics"
        table    = "errors"
      }
    },
    {
      http = {
        url     = "https://example.com/hooks/errors"
        headers = { Authorization = "Bearer ${var.webhook_token}" }
      }
    },
    {
      slack = {
        integration_id = "00000000-0000-0000-0000-000000000000"
        channel_name   = "#alerts"
      }
    },
  ]
}
```

## Schema

### Required

- `name` (String) - The workflow name.
- `sql` (String) - The SQL query the workflow runs.

### Optional

- `database` (String) - The database the SQL runs in. Defaults to the provider database.
- `description` (String) - Free-form description, up to 1000 characters.
- `schedule` (Attributes) - Automatic execution. Omit for a manual-only workflow. See [below](#nested-schema-for-schedule).
- `sinks` (Attributes List) - Where the workflow delivers its rows, at most five. Each sink sets exactly one of `table`, `http` or `slack`. See [below](#nested-schema-for-sinks).

### Read-Only

- `id` (String) - The workflow ID.

### Nested Schema for `schedule`

- `cron` (String, Required) - UTC cron expression, 5 fields or 6 with seconds (for example `*/30 * * * * *`).
- `enabled` (Boolean, Optional) - Set to `false` to pause scheduled runs. Default: `true`.

### Nested Schema for `sinks`

- `id` (String, Read-Only) - The sink ID.
- `table` (Attributes, Optional) - Insert the rows into a RawTree table.
  - `database` (String, Required)
  - `table` (String, Required)
- `http` (Attributes, Optional) - POST the rows to an HTTP endpoint.
  - `url` (String, Required, Sensitive) - HTTP or HTTPS URL.
  - `headers` (Map of String, Optional, Sensitive) - Request headers, at most 20. The API never returns header values, so the provider keeps the configured values in state and only detects drift in the header names.
- `slack` (Attributes, Optional) - Post the rows to a Slack channel.
  - `integration_id` (String, Required) - ID of a Slack integration connected to the organization (`rtree integration list`).
  - `channel_id` (String, Optional) - Slack channel ID, such as `C0123456789`. Set this or `channel_name`.
  - `channel_name` (String, Optional) - Slack channel name, such as `alerts` or `#alerts`. The API resolves it to an ID when the sink is saved.

## Import

Import a workflow by its ID:

```shell
terraform import rawtree_workflow.errors <workflow-id>
```

After an import the HTTP header values are unknown, so the next apply sends the configured header values once.
