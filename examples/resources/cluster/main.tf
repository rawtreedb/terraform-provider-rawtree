terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}

provider "rawtree" {
  # Uses RAWTREE_ACCESS_TOKEN and RAWTREE_ORG.
  # API keys (rt_...) cannot manage clusters.
}

# A dedicated cluster that autoscales each replica between 2 CPU / 8 GiB and
# 8 CPU / 32 GiB. `rtree cluster regions` and `rtree cluster sizes` list the
# valid regions and sizes.
resource "rawtree_cluster" "analytics" {
  name                 = "analytics"
  region_id            = "6f1c1f7e-0000-4000-8000-000000000001"
  replicas             = 2
  min_size             = "2:8"
  max_size             = "8:32"
  idle_timeout_minutes = 0
}
