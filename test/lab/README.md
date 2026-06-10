# Rawtree Ingestion Lab

Single CloudFront distribution with WAF + real-time logs enabled. Every request
generates both WAF log records and CloudFront real-time log records, ingested
into separate Rawtree tables via two independent Firehose pipelines.

## Architecture

```
                    Request
                       |
                       v
              CloudFront Distribution
              /                    \
  WAF (Web ACL)              Real-Time Logs
       |                          |
       v                          v
    Firehose                 Kinesis Data Stream
  (DirectPut)                     |
       |                          v
       v                       Firehose
  Rawtree API              (KinesisSource)
  (waf_logs)                      |
                                  v
                             Rawtree API
                          (cloudfront_logs)
```

## Prerequisites

- AWS credentials configured (`aws configure` or env vars)
- Rawtree account with an API key
- Provider binary built (see below)
- `~/.terraformrc` with dev_overrides pointing to the repo root

## Setup

```bash
# 1. Build the provider binary into the repo root (where dev_overrides points)
cd /path/to/terraform-provider-rawtree
go build -o . .

# 2. Set Rawtree credentials (provider reads these from env)
export RAWTREE_API_KEY="rw_..."
export RAWTREE_ORG="your-org"
export RAWTREE_PROJECT="your-project"

# 3. Bootstrap the lab (downloads aws + random providers)
cd test/lab
make init   # or see "Manual init" below

# 4. Apply
terraform apply
```

### Manual init

`terraform init` fails because the `rawtreedb/rawtree` provider isn't in the
public registry (dev_overrides supplies it at plan/apply time). To install only
the registry providers:

```bash
terraform providers lock -platform=$(go env GOOS)_$(go env GOARCH) hashicorp/aws hashicorp/random
# Then bootstrap the plugin cache from a temp config:
tmpdir=$(mktemp -d)
cat > "$tmpdir/main.tf" <<'EOF'
terraform { required_providers { aws = { source = "hashicorp/aws"; version = "~> 5.0" }; random = { source = "hashicorp/random"; version = "~> 3.0" } } }
EOF
terraform -chdir="$tmpdir" init && cp -r "$tmpdir/.terraform" .terraform && rm -rf "$tmpdir"
```

This creates:
- S3 origin bucket with sample content
- CloudFront distribution with WAF attached
- WAFv2 Web ACL with 8 managed + 2 custom rules
- `rawtree_waf_ingestion` pipeline (WAF -> Firehose -> Rawtree)
- `rawtree_cloudfront_ingestion` pipeline (CF -> Kinesis -> Firehose -> Rawtree)

## Generate Traffic

```bash
CLOUDFRONT_DOMAIN="$(terraform output -raw cloudfront_domain 2>/dev/null)"
```

### Option 1: Mixed traffic (legitimate + malicious)

```bash
./generate-traffic.sh "$CLOUDFRONT_DOMAIN" 20
```

Each request hits both pipelines simultaneously.

### Option 2: Heavy attack traffic (1000+ payloads)

```bash
docker run --rm wallarm/gotestwaf --url "https://$CLOUDFRONT_DOMAIN" --skipWAFBlockCheck
```

### Option 3: Legitimate-only traffic

```bash
./generate-traffic.sh "$CLOUDFRONT_DOMAIN" 100 --legit
```

## Custom Table Names

```bash
terraform apply -var waf_table=my_waf_logs -var cloudfront_table=my_cf_logs
```

## Teardown

```bash
terraform destroy
```
