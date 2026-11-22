terraform {
  required_providers {
    rawtree = {
      source = "rawtreedb/rawtree"
    }
  }
}

provider "rawtree" {}

resource "rawtree_workflow" "daily_events" {
  organization = "acme"
  cluster      = "production"
  name         = "daily-events"
  description  = "Count events each morning."

  query = {
    database = "analytics"
    sql      = "SELECT count() AS events FROM events"
  }

  schedule = {
    cron    = "0 8 * * *"
    enabled = false
  }
}
