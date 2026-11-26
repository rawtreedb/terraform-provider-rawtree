terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

provider "rawtree" {}

# Key for an ingestion pipeline: it can only insert into the analytics database.
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
