# Acceptance Test Setup

This directory contains everything needed to run acceptance tests for the Rawtree Terraform provider.

## Prerequisites

- AWS credentials configured (`aws configure` or env vars)
- Rawtree account with an API key
- Go 1.22+, Terraform 1.0+

## 1. Create the test bucket

```sh
cd test/setup
terraform init
terraform apply
```

This creates an S3 bucket with sample data in multiple formats:

```
s3://rawtree-provider-test-<suffix>/
├── data/json/events.json      # JSON array
├── data/json/logs.jsonl       # JSONL (newline-delimited)
└── data/csv/metrics.csv       # CSV
```

## 2. Run acceptance tests

Copy the output from `terraform apply` and add your Rawtree credentials:

```sh
# From terraform output
export RAWTREE_TEST_S3_BUCKET="rawtree-provider-test-..."
export AWS_REGION="us-east-1"

# Your Rawtree credentials
export RAWTREE_API_KEY="rw_..."
export RAWTREE_ORG="my-org"
export RAWTREE_PROJECT="my-project"

# Run tests
cd ../..
TF_ACC=1 go test -v -timeout 30m ./internal/resources/s3_ingestion/
```

## 3. Clean up

```sh
cd test/setup
terraform destroy
```

## Test data

The `data/` directory contains sample files used by the tests:

| File | Format | Description |
|------|--------|-------------|
| `events.json` | JSON | Array of page view/click/signup events |
| `metrics.csv` | CSV | Server metrics (CPU, memory, disk) |
| `logs.jsonl` | JSONL | Application log entries |

You can add more test files here — they'll be uploaded to S3 on next `terraform apply`.
