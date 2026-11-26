---
page_title: "rawtree_service_account Resource - Rawtree"
subcategory: ""
description: |-
  Manages a RawTree service account in the provider organization.
---

# rawtree_service_account (Resource)

Manages a service account in the provider organization. A service account is a non-human identity: a [`rawtree_federation_rule`](federation_rule.md) can let a CI job or other workload act as it without a stored API key.

The API has no update endpoint, so changing any argument replaces the service account (and gives it a new `id`).

~> **A replacement deletes dependent objects.** When RawTree deletes a service account, it also deletes the federation rules and API keys that use it as principal. Terraform creates the rules again with new IDs, so each workload that holds the old `federation_rule_id` stops working.

Requires the provider `access_token` and `organization`, and organization administrator rights. API keys (`rt_...`) cannot manage service accounts. The API accepts only a first-party session token for the deletion: with an OAuth access token, `terraform destroy` and each change that replaces the service account fail with 401.

~> **Interactive use only.** This resource needs a user access token, which expires after 15 minutes and which the provider cannot refresh. Run `terraform apply` from a workstation with a new token. API keys and workload federation tokens do not work, so this resource cannot run unattended in CI.

## Example Usage

```hcl
resource "rawtree_service_account" "ci" {
  name        = "github-deployer"
  description = "GitHub Actions deploy workflow"
}
```

See [`rawtree_federation_rule`](federation_rule.md) for a complete GitHub Actions example.

## Schema

### Required

- `name` (String) - Name, unique within the organization. Lowercase letters and digits separated by single hyphens, 1-128 characters. Changing it forces a new resource.

### Optional

- `description` (String) - Description, at most 2000 UTF-8 bytes. Changing it forces a new resource.
- `role` (String) - Stored organization role: `member` (default) or `admin`. Federation and API key permissions are configured separately. Changing it forces a new resource.

### Read-Only

- `id` (String) - Service account ID. Use it as `principal_id` in a federation rule.
- `organization_id` (String) - ID of the organization that owns the service account.

## Import

Import by service account ID (the provider `organization` must be the owning organization):

```shell
terraform import rawtree_service_account.ci 55555555-5555-5555-5555-555555555555
```
