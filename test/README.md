# Acceptance Test Setup

This directory contains everything needed to run acceptance tests for the Rawtree Terraform provider.

## Prerequisites

- AWS credentials configured (`aws configure` or env vars)
- Rawtree account with an API key
- Go 1.22+, Terraform 1.0+

## 1. Create the test infrastructure

```sh
cd test/setup
terraform init
terraform apply
```

This creates an S3 bucket with sample data, a WAFv2 Web ACL, and a CloudFront
distribution for testing:

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
export RAWTREE_TEST_WAF_WEB_ACL_ARN="arn:aws:wafv2:..."
export RAWTREE_TEST_CF_DISTRIBUTION_ID="E23M245BXXXXXX"
export AWS_REGION="us-east-1"

# Your Rawtree credentials
export RAWTREE_API_KEY="rw_..."
export RAWTREE_ORG="my-org"
export RAWTREE_PROJECT="my-project"

# Run S3 ingestion tests
TF_ACC=1 go test -v -timeout 30m ./internal/resources/s3_ingestion/

# Run WAF ingestion tests (all: unit + infra provisioning + E2E data validation)
TF_ACC=1 go test -v -timeout 45m ./internal/resources/waf_ingestion/

# Run CloudFront ingestion tests (all: unit + infra provisioning + E2E data validation)
TF_ACC=1 go test -v -timeout 60m ./internal/resources/cloudfront_ingestion/

# Run Supabase CDC ingestion lifecycle tests (no E2E data validation)
#   Required (in addition to RAWTREE_API_KEY/ORG/PROJECT and AWS creds):
#     RAWTREE_TEST_SUPABASE_DATABASE_URL  reachable Postgres connection string
#     RAWTREE_TEST_SUPABASE_SUBNETS       comma-separated subnet IDs in us-east-1
#   Optional:
#     RAWTREE_TEST_SUPABASE_SECURITY_GROUPS  comma-separated SG IDs
#
#   Account requirement: the ECS service-linked role AWSServiceRoleForECS must
#   exist (it's an account-wide singleton required for ECS Fargate). The
#   pre-check creates it idempotently with iam:CreateServiceLinkedRole on
#   ecs.amazonaws.com — make sure your test credentials are allowed that.
#
# These tests run with run_initialization_task=false and only validate the
# AWS resource lifecycle (cluster/service/task def/log group/role/secrets) —
# the worker container is not required to actually reach Postgres.
TF_ACC=1 go test -v -timeout 30m ./internal/resources/supabase_cdc_ingestion/

# Run the Supabase CDC end-to-end data test
#
#   This test exercises the full pipeline against the canonical Superstore
#   Sales dataset: it provisions its own dual-stack VPC + IPv6 subnet + IGW,
#   creates the rawtree_supabase_cdc_ingestion resource, inserts rows into
#   the source table, and polls Rawtree until they arrive. The schema is
#   hard-coded — full setup walkthrough (create Supabase project, import the
#   CSV, set env vars) is in test/supabase/README.md.
TF_ACC=1 go test -v -timeout 30m \
  -run TestAccSupabaseCDCIngestion_endToEndData \
  ./internal/resources/supabase_cdc_ingestion/
```

## 3. Firehose Transform Stub

The `firehose-stub/` directory contains a local HTTP server that implements the
Rawtree `?transform=firehose` endpoint and a minimal query endpoint. Use this
when the real Rawtree Firehose transform endpoint is not yet available, or for
local development and debugging.

### What it does

The stub speaks the [Kinesis Data Firehose HTTP endpoint protocol](https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html):

- Receives Firehose deliveries at `POST /v1/{org}/{project}/tables/{table}?transform=firehose`
- Validates `X-Amz-Firehose-Access-Key` header (optional)
- Decodes base64-encoded records and stores them in memory keyed by table
- Handles WAF logs that may contain newline-delimited JSON within a single record
- Deduplicates by `requestId` (Firehose retries use the same ID)
- Serves `POST /v1/{org}/{project}/query` for `SELECT count() as cnt FROM <table>` queries
- Serves `GET /debug/records` to dump all ingested records
- Serves `GET /healthz` for health checks

### Running the stub

Start in a separate terminal:

```sh
go run ./test/firehose-stub/ -port 9876 -access-key "$RAWTREE_API_KEY"
```

Then configure the provider to point at the stub:

```sh
export RAWTREE_URL=http://localhost:9876
```

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `9876` | Listen port |
| `-access-key` | (empty) | Expected `X-Amz-Firehose-Access-Key` value. Empty disables validation. |

**Important:** Firehose only delivers to HTTPS endpoints on port 443. The stub
is for local testing where Firehose is not involved (e.g., sending synthetic
requests directly). For real E2E tests with Firehose, you need to expose the
stub via an HTTPS reverse proxy or tunnel (e.g., ngrok, Cloudflare Tunnel).

### Testing the stub manually

```sh
# Health check
curl http://localhost:9876/healthz

# Simulate a Firehose delivery (base64-encode your JSON records)
curl -X POST 'http://localhost:9876/v1/myorg/myproj/tables/waf_logs?transform=firehose' \
  -H 'Content-Type: application/json' \
  -H 'X-Amz-Firehose-Access-Key: rw_...' \
  -d '{"requestId":"test-001","timestamp":1700000000000,"records":[{"data":"eyJ0aW1lc3RhbXAiOjEyMzR9"}]}'

# Query the count
curl -X POST 'http://localhost:9876/v1/myorg/myproj/query' \
  -H 'Content-Type: application/json' \
  -d '{"sql":"SELECT count() as cnt FROM waf_logs"}'

# Dump all records
curl http://localhost:9876/debug/records
```

## 4. Running individual E2E tests

Each resource package includes an end-to-end test that provisions real infrastructure,
generates traffic, and validates data arrives in Rawtree. These run as part of the
full suite above, but you can target them individually:

```sh
# WAF E2E only (~9 min — creates Firehose, generates WAF traffic, queries Rawtree)
TF_ACC=1 go test -v -timeout 45m -run TestAccWafIngestion_endToEndData ./internal/resources/waf_ingestion/

# CloudFront E2E only (~10 min — creates distribution + real-time logs, validates data)
TF_ACC=1 go test -v -timeout 60m -run TestAccCloudfrontIngestion_endToEndData ./internal/resources/cloudfront_ingestion/
```

## 5. Interactive lab (manual testing)

The `lab/` directory creates a single CloudFront distribution with WAF + real-time
logs, ingesting both into Rawtree. Use it for manual testing and demos.

```sh
cd test/lab
terraform apply
./generate-traffic.sh <cloudfront-domain>
```

See [lab/README.md](lab/README.md) for details.

## 6. Clean up

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
