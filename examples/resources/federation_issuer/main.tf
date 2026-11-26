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

# GitHub Actions OIDC. Keys come from OpenID discovery.
resource "rawtree_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
}

# An issuer without discovery, with an explicit JWKS URL.
resource "rawtree_federation_issuer" "internal" {
  name        = "internal-idp"
  description = "Internal identity provider"
  issuer_url  = "https://idp.example.com"
  jwks_source = "explicit_url"
  jwks_url    = "https://idp.example.com/keys"
}
