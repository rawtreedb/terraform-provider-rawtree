list "rawtree_workflow" "cluster" {
  provider = rawtree
  # Use Terraform's largest query limit to return every workflow.
  limit = 9223372036854775807

  config {
    organization = var.organization
    cluster      = var.cluster
  }
}
