terraform {
  required_providers {
    rawtree = {
      source = "rawtreedb/rawtree"
    }
  }
}

provider "rawtree" {
  api_url      = "https://example.invalid"
  api_key      = "fixture-only"
  organization = "test-org"
  project      = "test-project"
}

resource "rawtree_waf_ingestion" "logs" {
  table       = "waf_logs"
  region      = "us-east-1"
  web_acl_arn = "arn:aws:wafv2:us-east-1:000000000000:regional/webacl/test/00000000-0000-0000-0000-000000000000"
}
