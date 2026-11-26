---
page_title: "rawtree_cluster Resource - Rawtree"
subcategory: ""
description: |-
  Manages a dedicated RawTree cluster in the provider organization.
---

# rawtree_cluster (Resource)

Manages a dedicated cluster in the provider organization. The create request goes to the deployment of the chosen region, and Terraform then waits until the cluster reports ready (up to 30 minutes). If the cluster reaches the `error` phase or the wait times out, the resource is saved as tainted so the next apply replaces it.

Only `name`, `idle_timeout_minutes` and `deletion_protection` can change in place. Any other change replaces the cluster and destroys its data. `deletion_protection` is `true` by default and blocks the deletion and the replacement: set it to `false` and apply before you destroy or replace the cluster. Deleting the cluster returns at once; RawTree cleans up the infrastructure asynchronously. Stopping and resuming a cluster is not managed by this resource; the cluster pauses by itself after `idle_timeout_minutes`.

Requires the provider `access_token` and `organization`. API keys (`rt_...`) cannot manage clusters.

~> **Interactive use only.** This resource needs a user access token, which expires after 15 minutes and which the provider cannot refresh. Run `terraform apply` from a workstation with a new token. API keys and workload federation tokens do not work, so this resource cannot run unattended in CI.

## Example Usage

```hcl
resource "rawtree_cluster" "analytics" {
  name                 = "analytics"
  region_id            = "6f1c1f7e-0000-4000-8000-000000000001" # rtree cluster regions
  replicas             = 2
  min_size             = "2:8"  # rtree cluster sizes
  max_size             = "8:32"
  idle_timeout_minutes = 0
}
```

### Customer-owned S3 storage

```hcl
resource "rawtree_cluster" "byo_s3" {
  name      = "byo-s3"
  region_id = "6f1c1f7e-0000-4000-8000-000000000001"
  replicas  = 2
  min_size  = "4:16"

  s3_storage = {
    data_bucket    = "acme-rawtree-data"
    backups_bucket = "acme-rawtree-backups"
    role_arn       = "arn:aws:iam::123456789012:role/rawtree-storage"
  }
  external_id = "acme-rawtree"
}
```

## Schema

### Required

- `name` (String) - Cluster name. Can be changed in place.
- `region_id` (String) - Region to create the cluster in, from `rtree cluster regions`. The region name or cloud region is also accepted. A change to a different region forces a new resource; a different identifier of the same region does not.
- `replicas` (Number) - Number of replicas. Changing it forces a new resource.
- `min_size` (String) - Initial and minimum size per replica as `CPU_CORES:MEMORY_GIB`, for example `2:8`. `rtree cluster sizes` (or `GET /v1/clusters/sizes`) lists the valid sizes; the API rejects other values at apply time. Changing it forces a new resource.

### Optional

- `max_size` (String) - Maximum autoscaling size per replica as `CPU_CORES:MEMORY_GIB`. Defaults to `min_size`. Changing it forces a new resource.
- `idle_timeout_minutes` (Number) - Minutes without activity before the cluster pauses: `0` disables idling, else 15 to 43200 (30 days). Defaults to the API default. Can be changed in place.
- `deletion_protection` (Boolean) - When `true` (the default), Terraform refuses to delete the cluster. This also blocks a replacement.
- `s3_storage` (Object) - Customer-owned S3 storage. Omit for RawTree-managed storage. Requires `external_id`. Changing it forces a new resource.
  - `data_bucket` (String, Required) - Bucket for cluster data.
  - `data_path` (String) - Path prefix in the data bucket. Default `""`.
  - `backups_bucket` (String, Required) - Bucket for backups.
  - `backups_path` (String) - Path prefix in the backups bucket. Default `""`.
  - `role_arn` (String, Required) - IAM role that RawTree assumes to reach the buckets. The API does not return it, so it is not checked for drift. Changing it forces a new resource, except for the first apply after an import.
- `external_id` (String) - External ID RawTree sends when it assumes `role_arn`. Required with `s3_storage`, else generated when omitted. Changing it forces a new resource.
- `resource_tag` (String) - Value of the `rawtree.com/cluster` AWS resource tag. Generated when omitted. Changing it forces a new resource.

### Read-Only

- `id` (String) - Cluster ID.
- `status` (String) - Lifecycle phase, for example `ready`, `stopped` or `provisioning`.
- `deployment_id` (String) - Regional deployment that serves the cluster.
- `cloud_region` (String) - Cloud region of that deployment.
- `rawtree_version` (String) - RawTree version the cluster runs.
- `created_at` (String) - Creation timestamp.

## Drift

`name`, `idle_timeout_minutes`, `external_id`, `resource_tag` and the S3 buckets and paths are refreshed from the API. `replicas`, `min_size` and `max_size` are only read on import: a stopped or autoscaling cluster reports different resources, and that must not replace it.

## Import

Import with the cluster ID:

```shell
terraform import rawtree_cluster.analytics 8b6f0c1e-0000-4000-8000-000000000001
```

`region_id`, `replicas` and the sizes are filled from the API. `s3_storage.role_arn` is not returned by the API, so set it in the configuration; the first apply stores it without a replacement. An imported cluster is protected from deletion until you apply `deletion_protection = false`.
