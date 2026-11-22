terraform {
  required_providers {
    rawtree = {
      source = "rawtreedb/rawtree"
    }
  }
}

# Set RAWTREE_MANAGEMENT_TOKEN to a credential with database administration access.
provider "rawtree" {}

resource "rawtree_database" "analytics" {
  organization = "acme"
  cluster      = "production"
  name         = "analytics"
}
