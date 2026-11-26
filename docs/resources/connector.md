---
page_title: "rawtree_connector Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree connector that streams Kafka, PostgreSQL or NATS data into RawTree tables.
---

# rawtree_connector (Resource)

Manages a connector in a cluster. A connector reads from Kafka topics, a PostgreSQL publication or NATS streams and writes each source into a RawTree table (a destination).

Credentials (PostgreSQL password, Kafka SASL username and password, NATS password, token or seed) go in the write-only `credentials_json_wo` attribute. Terraform never stores it in state or plan files. The provider deep-merges it into `settings_json` whenever it sends settings. Because Terraform cannot see changes to a write-only value, bump `credentials_wo_version` to send new credentials. Write-only attributes need Terraform 1.11 or later.

Requires the provider `access_token` and `organization`. API keys (`rt_...`) cannot manage connectors.

~> **Interactive use only.** This resource needs a user access token, which expires after 15 minutes and which the provider cannot refresh. Run `terraform apply` from a workstation with a new token. API keys and workload federation tokens do not work, so this resource cannot run unattended in CI.

## Example Usage

### PostgreSQL

```hcl
resource "rawtree_connector" "orders" {
  name = "orders-cdc"
  type = "postgres"

  destinations = [{
    database        = "shop"
    table           = "orders"
    postgres_tables = ["public.orders"]
  }]

  settings_json = jsonencode({
    host                   = "db.example.com"
    database               = "shop"
    user                   = "rawtree"
    publication            = "rawtree_pub"
    slot                   = "rawtree_slot"
    create_slot_if_missing = true
  })

  credentials_json_wo    = jsonencode({ password = var.pg_password })
  credentials_wo_version = 1
}
```

### Kafka with SASL

```hcl
resource "rawtree_connector" "events" {
  cluster = "analytics"
  name    = "events"
  type    = "kafka"

  destinations = [{
    database = "default"
    table    = "events"
    topics   = ["events"]
  }]

  settings_json = jsonencode({
    bootstrap_servers = "broker-1:9092,broker-2:9092"
    tls               = { enabled = true }
    sasl              = { enabled = true, mechanism = "SCRAM-SHA-512" }
  })

  credentials_json_wo    = jsonencode({ sasl = { username = "rawtree", password = var.kafka_password } })
  credentials_wo_version = 1
}
```

## Schema

### Required

- `name` (String) - Connector name. Can be changed in place.
- `type` (String) - `kafka`, `postgres` or `nats`. Changing it forces a new resource.
- `destinations` (List of Object) - Destination tables. Can be changed in place; a destination keeps its ID while its `database` and `table` stay the same.
  - `database` (String, Required) - Destination database.
  - `table` (String, Required) - Destination table.
  - `topics` (List of String) - Kafka: topics to read.
  - `group_id` (String) - Kafka: consumer group. Generated when omitted.
  - `postgres_tables` (List of String) - PostgreSQL: source tables as `schema.table`.
  - `initial_load` (Boolean) - PostgreSQL: copy existing rows before streaming changes. Defaults to `true`. Only possible for destinations created together with the connector.
  - `nats_stream` (String) - NATS: stream name.
  - `nats_consumer` (String) - NATS: durable consumer name.
  - `id` (String, Read-Only) - Destination ID.
- `settings_json` (String) - Connector settings as a JSON object, without credentials. Key order and formatting do not cause diffs. Only the keys you set are checked for drift, so API defaults (for example `port`) do not show as changes.

### Optional

- `cluster` (String) - Cluster name or ID. Defaults to the provider `cluster`. Changing it forces a new resource, so prefer the ID (`rawtree_cluster.x.id`): a cluster name can change.
- `credentials_json_wo` (String, Sensitive, Write-only) - Credentials as a JSON object, merged into the settings.
- `credentials_wo_version` (Number) - Change it to send `credentials_json_wo` again.
- `status` (String) - `active` or `paused`. Can be changed in place.

### Read-Only

- `id` (String) - Connector ID.
- `credentials_configured` (Boolean) - Whether the API holds credentials for the connector.
- `slot_owned` (Boolean) - PostgreSQL: whether RawTree created the replication slot and drops it when the connector is deleted.
- `revision` (Number) - Connector revision.
- `created_at` (String) - Creation timestamp.
- `updated_at` (String) - Last update timestamp.

## Import

Import with `<cluster>/<connector_id>`:

```shell
terraform import rawtree_connector.orders analytics/7c9e6679-0000-4000-8000-000000000001
```

After import, `settings_json` holds every setting the API returns. The first apply sends your configured settings and credentials once.
