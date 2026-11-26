terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

provider "rawtree" {}

# Write-only key for an ingestion pipeline. `database` is the default database
# of the key. It does not restrict the key to that database.
resource "rawtree_api_key" "ingest" {
  name       = "firehose-ingest"
  database   = "analytics"
  permission = "write_only"
  expires_at = "2027-12-31T00:00:00Z"
}

output "ingest_token" {
  value     = rawtree_api_key.ingest.token
  sensitive = true
}
