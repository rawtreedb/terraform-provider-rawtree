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

This creates an S3 bucket with sample data and a WAFv2 Web ACL for testing:

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
export AWS_REGION="us-east-1"

# Your Rawtree credentials
export RAWTREE_API_KEY="rw_..."
export RAWTREE_ORG="my-org"
export RAWTREE_PROJECT="my-project"

# Run S3 ingestion tests
TF_ACC=1 go test -v -timeout 30m ./internal/resources/s3_ingestion/

# Run WAF ingestion tests (infrastructure-only)
TF_ACC=1 go test -v -timeout 30m ./internal/resources/waf_ingestion/

# Run WAF E2E test only (creates CloudFront + generates traffic)
TF_ACC=1 go test -v -timeout 45m -run TestAccWafIngestion_endToEndData ./internal/resources/waf_ingestion/
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

## 4. Clean up

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
