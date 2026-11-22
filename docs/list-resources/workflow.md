---
page_title: "rawtree_workflow List Resource - Tinybird RawTree"
subcategory: "Tinybird RawTree"
description: Discover SQL workflows in a Tinybird RawTree cluster.
---

# rawtree_workflow

Terraform 1.14 or later can discover existing workflows and generate resource and import blocks. Supply the organization and cluster in a `.tfquery.hcl` file.

```hcl
list "rawtree_workflow" "cluster" {
  provider = rawtree
  limit    = 100

  config {
    organization = "acme"
    cluster      = "production"
  }
}
```

Run `terraform query -generate-config-out=workflows.tf` with a new output filename. Review the generated configuration and run `terraform plan`. An import-only plan shows one import per selected workflow and zero additions, changes, or deletions.

Terraform omits sensitive HTTP URLs from generated configuration. Supply each existing URL through a sensitive variable before planning. Header values cannot be recovered from the API. Leave `headers` unset to preserve them. Keep the generated sink entries for every destination you want the workflow to retain.
