"""
Rawtree S3 Batch Ingestion - AWS Glue Job

This script lists all objects in a specified S3 bucket/prefix,
filters by file pattern, generates presigned URLs, and sends them
to the Rawtree API for direct ingestion using a thread pool for
concurrent processing.

Required Glue job parameters:
  --BUCKET, --FORMAT, --API_URL, --API_KEY, --TABLE

Optional Glue job parameters:
  --PREFIX, --FILE_PATTERN, --CONCURRENCY
"""

import sys
import re
import traceback
import urllib.parse
import urllib.request
import json
import threading
from concurrent.futures import ThreadPoolExecutor, as_completed
import boto3

print("Rawtree S3 Batch Ingestion starting...")


def get_args():
    """Parse Glue job arguments from sys.argv manually.

    getResolvedOptions requires ALL listed args to be present, which breaks
    when optional args have empty values. We parse manually instead.
    """
    args = {}
    argv = sys.argv[1:]  # Skip script name.
    i = 0
    while i < len(argv):
        if argv[i].startswith("--"):
            key = argv[i][2:]
            if i + 1 < len(argv) and not argv[i + 1].startswith("--"):
                args[key] = argv[i + 1]
                i += 2
            else:
                args[key] = ""
                i += 1
        else:
            i += 1
    return args


try:
    args = get_args()
    print(f"Parsed arguments: { {k: (v if k != 'API_KEY' else '***') for k, v in args.items()} }")

    bucket = args["BUCKET"]
    prefix = args.get("PREFIX", "")
    file_pattern = args.get("FILE_PATTERN", "")
    file_format = args["FORMAT"]
    api_url = args.get("API_URL", "").rstrip("/")
    api_key = args["API_KEY"]
    table = args.get("TABLE", "")
    ingest_endpoint = args.get("INGEST_ENDPOINT", "")
    concurrency = int(args.get("CONCURRENCY", "10") or "10")
except Exception as e:
    print(f"FATAL: Failed to parse arguments: {e}")
    traceback.print_exc()
    sys.exit(1)

# Compile file pattern regex if provided.
pattern = re.compile(file_pattern) if file_pattern else None

# Valid extensions for the format (including gzipped variants).
FORMAT_EXTENSIONS = {
    "json": (".json", ".json.gz", ".jsonl", ".jsonl.gz", ".ndjson", ".ndjson.gz"),
    "csv": (".csv", ".csv.gz"),
    "parquet": (".parquet", ".pq"),
}
valid_extensions = FORMAT_EXTENSIONS.get(file_format, ())

# Thread-safe counters.
lock = threading.Lock()
total = 0
errors = 0


def list_objects(bucket_name, prefix_str):
    """List all objects in bucket under prefix, excluding .rawtree/ internal prefix."""
    s3 = boto3.client("s3")
    paginator = s3.get_paginator("list_objects_v2")
    pages = paginator.paginate(Bucket=bucket_name, Prefix=prefix_str)

    for page in pages:
        for obj in page.get("Contents", []):
            key = obj["Key"]
            # Skip internal provider files.
            if key.startswith(".rawtree/"):
                continue
            yield key


def matches_filter(key):
    """Check if key matches file pattern and format extension."""
    if valid_extensions and not any(key.lower().endswith(ext) for ext in valid_extensions):
        return False
    if pattern and not pattern.search(key):
        return False
    return True


def ingest_key(key):
    """Generate a presigned URL and send it to the Rawtree API."""
    global total, errors

    # Each thread gets its own S3 client (boto3 clients are not thread-safe).
    s3 = boto3.client("s3")
    presigned_url = s3.generate_presigned_url(
        "get_object",
        Params={"Bucket": bucket, "Key": key},
        ExpiresIn=3600,
    )

    endpoint = ingest_endpoint if ingest_endpoint else f"{api_url}/v1/tables/{table}"
    params = urllib.parse.urlencode({"url": presigned_url})
    url = f"{endpoint}?{params}"

    req = urllib.request.Request(url, method="POST")
    req.add_header("Authorization", f"Bearer {api_key}")

    try:
        with urllib.request.urlopen(req, timeout=300) as response:
            # Rawtree returns an NDJSON event stream (started, progress, done/error).
            last_event = None
            for line in response.read().decode().splitlines():
                line = line.strip()
                if not line:
                    continue
                last_event = json.loads(line)
                if last_event.get("type") == "error":
                    raise RuntimeError(f"API error: {last_event}")
        with lock:
            total += 1
        print(f"OK: {key} -> {last_event}")
        return key, True
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        with lock:
            errors += 1
        print(f"ERROR: {key} -> HTTP {e.code}: {body}")
        return key, False
    except Exception as e:
        with lock:
            errors += 1
        print(f"ERROR: {key} -> {e}")
        return key, False


# Main execution.
try:
    print(f"Bucket: s3://{bucket}/{prefix}")
    print(f"Format: {file_format}, Pattern: {file_pattern or '(none)'}, Concurrency: {concurrency}")
    target = ingest_endpoint if ingest_endpoint else f"{api_url}/v1/tables/{table}"
    print(f"Target: {target}")

    # Collect matching keys.
    keys = [key for key in list_objects(bucket, prefix) if matches_filter(key)]
    print(f"Found {len(keys)} matching files")

    if not keys:
        print("No files to ingest. Done.")
        sys.exit(0)

    # Process concurrently.
    with ThreadPoolExecutor(max_workers=concurrency) as executor:
        futures = {executor.submit(ingest_key, key): key for key in keys}
        for future in as_completed(futures):
            future.result()

    print(f"Batch ingestion complete. Total: {total}, Errors: {errors}")

    if errors > 0:
        sys.exit(1)

except Exception as e:
    print(f"FATAL: {e}")
    traceback.print_exc()
    sys.exit(1)
