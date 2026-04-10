"""
Rawtree S3 Event Ingestion - AWS Lambda Handler

Triggered by EventBridge on S3 ObjectCreated events. Generates a presigned
URL for the new object and sends it to the Rawtree API for direct ingestion.

Environment variables:
  API_URL: Rawtree API base URL
  API_KEY: Rawtree API key
  ORG: Rawtree organization name
  PROJECT: Rawtree project name
  TABLE: Rawtree table name
  FORMAT: Expected file format (json, csv, parquet)
  FILE_PATTERN: Optional regex pattern for filtering keys
  PREFIX: Optional S3 key prefix filter
"""

import os
import re
import json
import urllib.parse
import urllib.request
import boto3

s3_client = boto3.client("s3")

# Configuration from environment.
API_URL = os.environ["API_URL"].rstrip("/")
API_KEY = os.environ["API_KEY"]
ORG = os.environ["ORG"]
PROJECT = os.environ["PROJECT"]
TABLE = os.environ["TABLE"]
FORMAT = os.environ.get("FORMAT", "")
FILE_PATTERN = os.environ.get("FILE_PATTERN", "")
PREFIX = os.environ.get("PREFIX", "")

# Compile regex once (cold start).
PATTERN = re.compile(FILE_PATTERN) if FILE_PATTERN else None

FORMAT_EXTENSIONS = {
    "json": (".json", ".json.gz", ".jsonl", ".jsonl.gz", ".ndjson", ".ndjson.gz"),
    "csv": (".csv", ".csv.gz"),
    "parquet": (".parquet", ".pq"),
}
VALID_EXTENSIONS = FORMAT_EXTENSIONS.get(FORMAT, ())


def handler(event, context):
    """Lambda entry point for EventBridge S3 events."""
    detail = event.get("detail", {})
    bucket = detail.get("bucket", {}).get("name", "")
    key = detail.get("object", {}).get("key", "")

    if not bucket or not key:
        print(f"Invalid event, missing bucket or key: {json.dumps(event)}")
        return {"status": "skipped", "reason": "invalid event"}

    # URL-decode the key (S3 events encode special characters).
    key = urllib.parse.unquote_plus(key)

    # Check prefix filter.
    if PREFIX and not key.startswith(PREFIX):
        print(f"Skipping {key}: does not match prefix {PREFIX}")
        return {"status": "skipped", "reason": "prefix mismatch"}

    # Check format extension.
    if VALID_EXTENSIONS and not any(key.lower().endswith(ext) for ext in VALID_EXTENSIONS):
        print(f"Skipping {key}: does not match format {FORMAT}")
        return {"status": "skipped", "reason": "format mismatch"}

    # Check regex pattern.
    if PATTERN and not PATTERN.search(key):
        print(f"Skipping {key}: does not match pattern {FILE_PATTERN}")
        return {"status": "skipped", "reason": "pattern mismatch"}

    # Generate presigned URL (1 hour expiry).
    presigned_url = s3_client.generate_presigned_url(
        "get_object",
        Params={"Bucket": bucket, "Key": key},
        ExpiresIn=3600,
    )

    # Send to Rawtree API.
    endpoint = f"{API_URL}/{ORG}/{PROJECT}/tables/{TABLE}"
    params = urllib.parse.urlencode({"url": presigned_url})
    url = f"{endpoint}?{params}"

    req = urllib.request.Request(url, method="POST")
    req.add_header("Authorization", f"Bearer {API_KEY}")
    req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req) as response:
            result = json.loads(response.read().decode())
            print(f"Ingested s3://{bucket}/{key}: {result}")
            return {"status": "ingested", "key": key, "result": result}
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"Error ingesting s3://{bucket}/{key}: {e.code} {body}")
        raise
