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

resource "rawtree_service_account" "ci" {
  name        = "github-deployer"
  description = "GitHub Actions deploy workflow"
}

# An organization admin service account.
resource "rawtree_service_account" "ops" {
  name = "ops-automation"
  role = "admin"
}
