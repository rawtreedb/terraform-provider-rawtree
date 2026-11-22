list "rawtree_database" "cluster" {
  provider = rawtree
  # Use Terraform's largest query limit to return every matching database.
  limit = 9223372036854775807

  config {
    organization = var.organization
    cluster      = var.cluster
  }
}
