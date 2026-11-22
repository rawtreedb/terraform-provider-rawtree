terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}

# Authentication can be configured here or via environment variables:
#   RAWTREE_API_KEY, RAWTREE_URL, RAWTREE_ORG, RAWTREE_DATABASE
#
# If you've logged in with the rtree CLI, credentials are auto-detected.
provider "rawtree" {
  api_key      = var.rawtree_api_key
  organization = var.rawtree_org
  database     = var.rawtree_database
}

variable "rawtree_api_key" {
  type      = string
  sensitive = true
}

variable "rawtree_org" {
  type = string
}

variable "rawtree_database" {
  type = string
}
