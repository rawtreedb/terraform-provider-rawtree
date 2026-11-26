---
page_title: "rawtree_federation_rule Resource - Rawtree"
subcategory: ""
description: |-
  Manages a workload federation rule on a RawTree cluster.
---

# rawtree_federation_rule (Resource)

Manages a workload federation rule on a cluster. A rule lets a token from a [`rawtree_federation_issuer`](federation_issuer.md) act as a user or [`rawtree_service_account`](service_account.md) when the token matches one of the allowed audiences and every claim condition. The workload sends the rule `id` with its OIDC token to the token exchange (`https://api.rawtree.com/oauth/token`) and gets a short-lived RawTree token. No RawTree API key is stored in the workload. See the [authentication documentation](https://rawtree.com/docs/reference/authentication).

All arguments except `cluster` can be changed in place.

Requires the provider `access_token`, `organization` and a cluster, and organization administrator rights.

~> **Interactive use only.** This resource needs a user access token, which expires after 15 minutes and which the provider cannot refresh. Run `terraform apply` from a workstation with a new token. API keys and workload federation tokens do not work, so this resource cannot run unattended in CI.

## Example Usage

### GitHub Actions

A service account, the GitHub Actions issuer, and a rule that lets the `deploy.yml` workflow on the `main` branch of one repository act as the service account.

```hcl
# Identity the GitHub workflow acts as.
resource "rawtree_service_account" "deployer" {
  name        = "github-deployer"
  description = "acme/app deploy workflow"
}

# Trust GitHub Actions OIDC tokens. Keys come from OpenID discovery.
resource "rawtree_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
}

# Exchange tokens from the main branch of one repository for a RawTree token
# of the service account. Pass rawtree_federation_rule.deploy.id to the
# token exchange in the workflow.
resource "rawtree_federation_rule" "deploy" {
  name      = "acme-app-main"
  issuer_id = rawtree_federation_issuer.github.id

  # GitHub's default audience is the URL of the repository owner. Use the
  # same value the workflow requests.
  allowed_audiences = ["https://github.com/acme"]

  # Exact matches on GitHub token claims. Numeric IDs survive repository
  # renames and transfers.
  claim_conditions = {
    repository_id       = "123456789"
    repository_owner_id = "987654321"
    ref                 = "refs/heads/main"
    workflow_ref        = "acme/app/.github/workflows/deploy.yml@refs/heads/main"
  }

  principal_type = "service_account"
  principal_id   = rawtree_service_account.deployer.id

  database_roles = ["deployer"]
  database       = "analytics"
}

output "federation_rule_id" {
  value = rawtree_federation_rule.deploy.id
}
```

The workflow needs `permissions: id-token: write` to request a GitHub OIDC token. GitHub tokens include `repository_id`, `repository_owner_id`, `ref`, `workflow_ref`, `environment` and `sub`. If you match on `sub`, copy its exact format from the repository's Actions OIDC settings: repositories can use a subject format that includes numeric IDs.

## Schema

### Required

- `name` (String) - Name. Lowercase letters and digits separated by single hyphens, 1-128 characters.
- `issuer_id` (String) - ID of the federation issuer that signs the tokens.
- `allowed_audiences` (Set of String) - Exact audiences. A token must match one of them.
- `claim_conditions` (Map of String) - Exact matches on top-level string claims. All must match; no wildcards. 1-32 entries; `iss`, `aud`, `exp`, `nbf`, `iat` and `jti` are not allowed.
- `principal_type` (String) - Identity the workload acts as: `service_account` or `user`.
- `principal_id` (String) - ID of the service account (`rawtree_service_account.<name>.id`) or user.
- `database` (String) - Default database for the workload. It does not restrict database-role grants.

### Optional

- `cluster` (String) - Cluster name or ID. Defaults to the provider `cluster`. Changing it forces a new resource, so prefer the ID (`rawtree_cluster.x.id`): a cluster name can change.
- `description` (String) - Description, at most 2000 UTF-8 bytes.
- `permission` (String) - Permission level, as for API keys: `admin`, `read_write`, `write_only` or `read_only`. Exactly one of `permission` or `database_roles` is required.
- `database_roles` (Set of String) - Database roles to grant. Exactly one of `permission` or `database_roles` is required.
- `enabled` (Boolean) - Whether the rule permits token exchange. Default: `true`.

### Read-Only

- `id` (String) - Rule ID. Workloads pass it to the token exchange.
- `organization_id` (String) - ID of the organization that owns the rule.
- `cluster_id` (String) - ID of the cluster that owns the rule.
- `created_at` (String) - Creation time.
- `updated_at` (String) - Last update time.

## Import

Import with `<cluster>/<id>`, where cluster is a name or ID:

```shell
terraform import rawtree_federation_rule.deploy production/44444444-4444-4444-4444-444444444444
```
