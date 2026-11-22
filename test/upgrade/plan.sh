#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/config"
go -C "$root" build -o "$work/bin/terraform-provider-rawtree" .
cp "$root/test/upgrade/main.tf" "$work/config/main.tf"
cat > "$work/terraform.rc" <<CONFIG
provider_installation {
  dev_overrides { "rawtreedb/rawtree" = "$work/bin" }
  direct {}
}
CONFIG
# A v0.1.0 Firehose destination migrates in place. Test both provider spellings.
export TF_CLI_CONFIG_FILE="$work/terraform.rc"
for attribute in project database; do
  cp "$root/test/upgrade/waf-v0.1.0.json" "$work/config/terraform.tfstate"
  sed "s/^  project /  $attribute /" "$root/test/upgrade/main.tf" > "$work/config/main.tf"
  terraform -chdir="$work/config" plan -input=false -refresh=false -out="$work/upgrade.plan"
  terraform -chdir="$work/config" show -json "$work/upgrade.plan" > "$work/upgrade.json"
  python3 - "$work/upgrade.json" "$work/config/terraform.tfstate" <<'PYTHON'
import json
import sys

with open(sys.argv[1]) as plan:
    (resource,) = json.load(plan)["resource_changes"]
change = resource["change"]
assert resource["address"] == "rawtree_waf_ingestion.logs"
assert change["actions"] == ["update"]
before, after = change["before"], change["after"]
assert after["id"] == before["id"]
assert after["database"] == after["project"] == before["project"]
assert after["endpoint_url"] == "https://example.invalid/v1/tables/waf_logs?database=test-project&transform=firehose"
# Model the post-apply state to verify that changing spellings causes no remote action.
with open(sys.argv[2]) as source:
    migrated = json.load(source)
migrated["resources"][0]["instances"][0]["attributes"] = after
with open(sys.argv[2], "w") as output:
    json.dump(migrated, output)
PYTHON
  terraform -chdir="$work/config" plan -input=false -refresh=false -detailed-exitcode
done
