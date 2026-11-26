terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.2"
    }
  }
}

provider "rawtree" {}

resource "rawtree_table" "events" {
  database    = "analytics"
  name        = "events"
  sorting_key = "region, toStartOfHour(timestamp)"
  ttl         = "toDateTime(ifNull(dynamicElement(timestamp, 'Int64'), 0)) + INTERVAL 30 DAY"

  settings_json = jsonencode({
    merge_with_ttl_timeout = 14400
  })
}
