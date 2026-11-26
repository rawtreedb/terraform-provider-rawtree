terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}

provider "rawtree" {
  # Uses RAWTREE_ACCESS_TOKEN, RAWTREE_ORG and RAWTREE_CLUSTER.
  # API keys (rt_...) cannot manage service accounts or federation.
}

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
