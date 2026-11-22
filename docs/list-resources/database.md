---
page_title: "rawtree_database List Resource - Tinybird RawTree"
subcategory: "Tinybird RawTree"
description: Discover databases in an existing cluster and generate Terraform configuration.
---

# rawtree_database

Use Terraform 1.14 or later to discover databases in one Tinybird RawTree cluster and generate their resource and import blocks in one command.

Configure the Tinybird RawTree provider with a credential that can list databases in the target cluster. Add the following to `databases.tfquery.hcl`, replacing the organization and cluster names.

```hcl
list "rawtree_database" "cluster" {
  provider = rawtree
  limit    = 9223372036854775807

  config {
    organization = "acme"
    cluster      = "production"
  }
}
```

Both names are required. Terraform defaults to 100 results per list block. The example uses Terraform's largest query limit to include every matching database. The provider skips built-in databases such as `default` and `system` and names that the managed resource cannot import.

Run these commands in the directory containing your provider configuration. Choose a new filename for the generated configuration.

```sh
terraform init
terraform query -generate-config-out=databases.tf
terraform plan
```

Generation reads the cluster and writes a local file. Review the generated blocks and keep the databases you want this Terraform state to own. For an import-only plan, expect one import per selected database and zero additions, changes, or deletions. Run `terraform apply` to record those imports in state.
