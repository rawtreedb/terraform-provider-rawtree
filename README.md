# Terraform Provider for Rawtree

The Rawtree Terraform provider enables automated data ingestion from AWS S3 into [Rawtree](https://rawtree.dev), a schemaless analytical database.

## Features

- **Batch ingestion**: Automatically ingest all existing objects from an S3 bucket/prefix into a Rawtree table using AWS Glue
- **Ongoing ingestion**: Set up EventBridge + Lambda to automatically ingest new objects as they arrive
- **Format support**: JSON, CSV, and Parquet (including gzipped variants)
- **Presigned URL ingestion**: Files are ingested via presigned URLs — Rawtree downloads directly from S3, no data passes through Glue/Lambda

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.22 (to build the provider)
- AWS credentials configured (for creating Glue, Lambda, EventBridge resources)
- A [Rawtree](https://rawtree.dev) account with an API key

## Installation

```hcl
terraform {
  required_providers {
    rawtree = {
      source  = "rawtreedb/rawtree"
      version = "~> 0.1"
    }
  }
}
```

## Authentication

The provider resolves credentials in this order:

1. Provider configuration block
2. Environment variables (`RAWTREE_API_KEY`, `RAWTREE_URL`, `RAWTREE_ORG`, `RAWTREE_PROJECT`)
3. Rawtree CLI config file (`~/.config/rtree/config.json` — created by `rtree login`)

```hcl
provider "rawtree" {
  api_key      = var.rawtree_api_key    # or set RAWTREE_API_KEY
  organization = "my-org"               # or set RAWTREE_ORG
  project      = "my-project"           # or set RAWTREE_PROJECT
}
```

## Usage

```hcl
resource "rawtree_s3_ingestion" "events" {
  table        = "events"
  bucket       = "my-data-bucket"
  prefix       = "data/events/"
  file_pattern = ".*\\.json$"
  format       = "json"
  region       = "us-east-1"
}
```

This creates:

1. An **AWS Glue job** that lists all matching files in `s3://my-data-bucket/data/events/`, generates presigned URLs, and sends them to the Rawtree API for ingestion (runs once on `terraform apply`)
2. An **AWS Lambda function** + **EventBridge rule** that automatically ingests each new matching object as it's created

### What gets created in your AWS account

| Resource | Purpose |
|----------|---------|
| IAM Role (Glue) | Allows Glue to read from your S3 bucket |
| IAM Role (Lambda) | Allows Lambda to read from your S3 bucket |
| S3 Bucket | Stores the Glue job script |
| Glue Job | One-time batch ingestion of existing objects |
| Lambda Function | Ongoing per-object ingestion |
| EventBridge Rule | Triggers Lambda on new S3 objects |

All resources are tagged with `managed-by: terraform-provider-rawtree` and fully cleaned up on `terraform destroy`.

## Development

### Building

```sh
go build -v ./...
```

### Testing

```sh
# Unit tests
go test -v ./...

# Acceptance tests (requires AWS + Rawtree credentials)
TF_ACC=1 go test -v ./...
```

### Using a local build

Add this to your `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "rawtreedb/rawtree" = "/path/to/terraform-provider-rawtree"
  }
  direct {}
}
```

## License

[Mozilla Public License v2.0](LICENSE)
