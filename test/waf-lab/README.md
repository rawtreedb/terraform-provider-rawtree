# WAF Lab

Production-like CloudFront + WAF setup for generating real WAF log records.

## Setup

```bash
cd test/waf-lab
terraform apply
```

Grab the CloudFront domain from the outputs.

## Generate Traffic

### Option 1: Custom mixed traffic (legitimate + malicious)

```bash
./generate-traffic.sh <cloudfront-domain> 20
```

### Option 2: Heavy attack traffic (1000+ payloads)

```bash
docker run --rm wallarm/gotestwaf --url https://<cloudfront-domain> --skipWAFBlockCheck
```
