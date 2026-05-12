terraform {
  required_providers {
    rawtree = {
      source = "rawtreedb/rawtree"
    }
  }
}

provider "rawtree" {
  # api_key      = "rw_..."           # or set RAWTREE_API_KEY
  # organization = "my-org"           # or set RAWTREE_ORG
  # project      = "my-project"       # or set RAWTREE_PROJECT
}

# Deploy a MongoDB CDC connector that replicates data from MongoDB to Rawtree.
# Runs as an ECS Fargate service in the specified AWS region.
resource "rawtree_mongo_connector" "example" {
  table          = "mongo_replication"
  mongo_uri      = "mongodb+srv://user:pass@cluster.mongodb.net/?retryWrites=true"
  mongo_database = "ecommerce"
  region         = "us-east-1"

  # Optional: only watch specific collections (comma-separated).
  collections = "orders,users,products"

  # Optional: customize table prefix (default: "mongo_").
  # Each collection maps to: {table_prefix}{collection_name}
  # e.g., orders → mongo_orders
  table_prefix = "mongo_"

  # Optional: change stream configuration.
  full_document    = "updateLookup" # or "whenAvailable" for MongoDB 6.0+
  snapshot_enabled = true

  # Optional: batching configuration.
  batch_max_rows = 4000
  flush_interval = "5s"

  # Optional: connector image version.
  image_tag = "latest"
}

output "ecs_cluster_arn" {
  value = rawtree_mongo_connector.example.ecs_cluster_arn
}

output "ecs_service_arn" {
  value = rawtree_mongo_connector.example.ecs_service_arn
}

output "log_group" {
  value = rawtree_mongo_connector.example.log_group_name
}
