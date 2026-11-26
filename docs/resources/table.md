---
page_title: "rawtree_table Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree table.
---

# rawtree_table (Resource)

Manages a RawTree table. Columns are not declared: the table creates them from the ingested data. The sorting key, TTL, settings and `replacing_enable` are updated in place, one API request for each changed field. A TTL can be changed but not removed: the API has no operation that removes it.

## Example Usage

```hcl
resource "rawtree_table" "events" {
  database    = "analytics"
  name        = "events"
  sorting_key = "region, toStartOfHour(timestamp)"
  ttl         = "toDateTime(ifNull(dynamicElement(timestamp, 'Int64'), 0)) + INTERVAL 30 DAY"

  settings_json = jsonencode({
    merge_with_ttl_timeout = 14400
  })
}
```

## Schema

### Required

- `name` (String) - Table name. Changing this forces a new resource.

### Optional

- `database` (String) - Database of the table. Defaults to the provider `database`, else the API key's database. Changing this forces a new resource.
- `sorting_key` (String) - Comma-separated SQL sorting expressions. Empty lets the table pick a key per part. Existing parts keep the previous key until they are merged.
- `ttl` (String) - Row-deletion TTL expression, optionally followed by `DELETE WHERE <condition>`. The API cannot remove a TTL: after it is set, you can change it but not unset it.
- `settings_json` (String) - Engine settings as a JSON object of booleans, numbers or strings. Key order and whitespace are ignored.
- `replacing_enable` (Boolean) - Replace rows with the same sorting key on merge. Null when the table engine does not support it.

### Read-Only

- `id` (String) - `<database>/<table>`, or `<table>` when the database is not known.
- `stored_sorting_key` (String) - Sorting key as the engine stores it.
- `stored_ttl` (String) - TTL as the engine stores it.

## Notes

- The engine rewrites expressions, for example `INTERVAL 30 DAY` becomes `toIntervalDay(30)`. The provider keeps your expression and compares later reads with `stored_sorting_key` and `stored_ttl`, so only changes made outside Terraform show a diff.
- Columns are `Dynamic`. A TTL must read a typed value, for example `dynamicElement(timestamp, 'Int64')`; the API rejects `timestamp + INTERVAL 30 DAY`.
- Some settings, for example `index_granularity`, can be set only when the table is created. The API rejects a later change.
- The API masks setting values, so changes to settings made outside Terraform are not detected. Removing a key from `settings_json` does not reset the setting; run `ALTER TABLE ... RESET SETTING <name>` with the Query API.

## Import

Import a table as `<database>/<table>`, or as `<table>` to use the provider database:

```shell
terraform import rawtree_table.events analytics/events
```

`settings_json` is empty after import; the next apply sends the configured settings. If you import with only `<table>` and no provider `database`, `database` is empty in state; the first value you set is stored without a replacement.
