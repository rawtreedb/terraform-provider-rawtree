"""
Rawtree S3 Batch Ingestion - AWS Glue Job

This script lists all objects in a specified S3 bucket/prefix,
filters by file pattern, generates presigned URLs, and sends them
to the Rawtree API for direct ingestion.

Arguments (passed via Glue job parameters):
  --BUCKET: S3 bucket name
  --PREFIX: S3 key prefix (optional)
  --FILE_PATTERN: Regex pattern for filtering keys (optional)
  --FORMAT: File format (json, csv, parquet)
  --API_URL: Rawtree API base URL
  --API_KEY: Rawtree API key
  --ORG: Rawtree organization name
  --PROJECT: Rawtree project name
  --TABLE: Rawtree table name
"""

import sys
import re
import urllib.parse
import urllib.request
import json
import boto3
from awsglue.utils import getResolvedOptions

args = getResolvedOptions(sys.argv, [
    "BUCKET", "PREFIX", "FILE_PATTERN", "FORMAT",
    "API_URL", "API_KEY", "ORG", "PROJECT", "TABLE",
])

bucket = args["BUCKET"]
prefix = args.get("PREFIX", "")
file_pattern = args.get("FILE_PATTERN", "")
file_format = args["FORMAT"]
api_url = args["API_URL"].rstrip("/")
api_key = args["API_KEY"]
org = args["ORG"]
project = args["PROJECT"]
table = args["TABLE"]

s3_client = boto3.client("s3")

# Compile file pattern regex if provided.
pattern = re.compile(file_pattern) if file_pattern else None

# Valid extensions for the format (including gzipped variants).
FORMAT_EXTENSIONS = {
    "json": (".json", ".json.gz", ".jsonl", ".jsonl.gz", ".ndjson", ".ndjson.gz"),
    "csv": (".csv", ".csv.gz"),
    "parquet": (".parquet", ".pq"),
}
valid_extensions = FORMAT_EXTENSIONS.get(file_format, ())


def list_objects(bucket_name, prefix_str):
    """List all objects in bucket under prefix."""
    paginator = s3_client.get_paginator("list_objects_v2")
    pages = paginator.paginate(Bucket=bucket_name, Prefix=prefix_str)

    for page in pages:
        for obj in page.get("Contents", []):
            yield obj["Key"]


def matches_filter(key):
    """Check if key matches file pattern and format extension."""
    # Check format extension.
    if valid_extensions and not any(key.lower().endswith(ext) for ext in valid_extensions):
        return False

    # Check regex pattern.
    if pattern and not pattern.search(key):
        return False

    return True


def generate_presigned_url(bucket_name, key, expiry=3600):
    """Generate a presigned URL for an S3 object."""
    return s3_client.generate_presigned_url(
        "get_object",
        Params={"Bucket": bucket_name, "Key": key},
        ExpiresIn=expiry,
    )


def ingest_to_rawtree(presigned_url):
    """Send a presigned URL to Rawtree API for ingestion."""
    endpoint = f"{api_url}/{org}/{project}/tables/{table}"
    params = urllib.parse.urlencode({"url": presigned_url})
    url = f"{endpoint}?{params}"

    req = urllib.request.Request(url, method="POST")
    req.add_header("Authorization", f"Bearer {api_key}")
    req.add_header("Content-Type", "application/json")

    with urllib.request.urlopen(req) as response:
        return json.loads(response.read().decode())


# Main execution.
total = 0
errors = 0

print(f"Starting batch ingestion from s3://{bucket}/{prefix}")
print(f"Format: {file_format}, Pattern: {file_pattern or '(none)'}")

for key in list_objects(bucket, prefix):
    if not matches_filter(key):
        continue

    try:
        url = generate_presigned_url(bucket, key)
        result = ingest_to_rawtree(url)
        total += 1
        print(f"Ingested: {key} -> {result}")
    except Exception as e:
        errors += 1
        print(f"Error ingesting {key}: {e}")

print(f"Batch ingestion complete. Total: {total}, Errors: {errors}")

if errors > 0:
    sys.exit(1)
