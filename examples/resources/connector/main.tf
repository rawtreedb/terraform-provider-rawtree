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
  # API keys (rt_...) cannot manage connectors.
}

variable "pg_password" {
  type      = string
  sensitive = true
}

# Stream changes of public.orders into shop.orders. The password is
# write-only: bump credentials_wo_version to send a new one.
resource "rawtree_connector" "orders" {
  name = "orders-cdc"
  type = "postgres"

  destinations = [{
    database        = "shop"
    table           = "orders"
    postgres_tables = ["public.orders"]
  }]

  settings_json = jsonencode({
    host                   = "db.example.com"
    database               = "shop"
    user                   = "rawtree"
    publication            = "rawtree_pub"
    slot                   = "rawtree_slot"
    create_slot_if_missing = true
  })

  credentials_json_wo    = jsonencode({ password = var.pg_password })
  credentials_wo_version = 1
}
