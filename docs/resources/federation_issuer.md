---
page_title: "rawtree_federation_issuer Resource - Rawtree"
subcategory: ""
description: |-
  Manages a workload federation issuer (an external OIDC identity provider).
---

# rawtree_federation_issuer (Resource)

Manages a workload federation issuer in the provider organization. An issuer is an external OIDC identity provider, such as GitHub Actions, whose tokens a [`rawtree_federation_rule`](federation_rule.md) can exchange for a RawTree token. Saving an issuer makes no requests to the provider.

All arguments can be changed in place. Rules that reference the issuer must be deleted before the issuer; Terraform does this when the rule references `rawtree_federation_issuer.<name>.id`.

Requires the provider `access_token` and `organization`, and organization administrator rights.

~> **Interactive use only.** This resource needs a user access token, which expires after 15 minutes and which the provider cannot refresh. Run `terraform apply` from a workstation with a new token. API keys and workload federation tokens do not work, so this resource cannot run unattended in CI.

## Example Usage

```hcl
resource "rawtree_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
}
```

### With an Explicit JWKS URL

```hcl
resource "rawtree_federation_issuer" "internal" {
  name        = "internal-idp"
  issuer_url  = "https://idp.example.com"
  jwks_source = "explicit_url"
  jwks_url    = "https://idp.example.com/keys"
}
```

## Schema

### Required

- `name` (String) - Name. Lowercase letters and digits separated by single hyphens, 1-128 characters.
- `issuer_url` (String) - Exact expected token issuer (`iss` claim). HTTPS URL without credentials, query or fragment. Stored without normalization, so a trailing slash matters.

### Optional

- `description` (String) - Description, at most 2000 UTF-8 bytes.
- `jwks_source` (String) - Where RawTree finds the signing keys: `discovery` (default, OpenID discovery from `issuer_url`) or `explicit_url`.
- `jwks_url` (String) - HTTPS JWKS URL. Required when `jwks_source` is `explicit_url`, not allowed with `discovery`.

### Read-Only

- `id` (String) - Issuer ID. Use it as `issuer_id` in a federation rule.
- `organization_id` (String) - ID of the organization that owns the issuer.
- `created_at` (String) - Creation time.
- `updated_at` (String) - Last update time.

## Import

Import by issuer ID (the provider `organization` must be the owning organization):

```shell
terraform import rawtree_federation_issuer.github 22222222-2222-2222-2222-222222222222
```
