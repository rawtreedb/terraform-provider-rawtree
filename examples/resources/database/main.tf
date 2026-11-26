terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

provider "rawtree" {}

resource "rawtree_database" "analytics" {
  name = "analytics"
}

# Tables in this database store data in a customer-owned bucket.
resource "rawtree_database" "byob" {
  name = "byob"

  s3_storage = {
    data_bucket    = "my-rawtree-data"
    data_path      = "rawtree/"
    backups_bucket = "my-rawtree-backups"
    role_arn       = "arn:aws:iam::123456789012:role/rawtree-storage"
  }
}
