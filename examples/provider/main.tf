terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

# Data-plane resources and the AWS ingestion resources use an API key.
# The key selects its organization, cluster and default database.
# Environment variables: RAWTREE_API_KEY, RAWTREE_API_URL, RAWTREE_DATABASE.
# If you logged in with the rtree CLI, the provider reads its config file.
provider "rawtree" {
  api_key  = var.rawtree_api_key
  database = "analytics" # optional, defaults to the API key's database
}

variable "rawtree_api_key" {
  type      = string
  sensitive = true
}
