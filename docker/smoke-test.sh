#!/bin/bash
# Checks a running stack from the outside, the way a visitor reaches it.
#
#   docker/smoke-test.sh [base-url] [admin-key]
#
# Defaults: http://localhost:$HTTP_PORT and ADMIN_KEY from the environment.
# CI runs it after `docker compose up`; you can run it too (Git Bash/WSL).

BASE=${1:-http://localhost:${HTTP_PORT:-8000}}
KEY=${2:-$ADMIN_KEY}
HOST=$(echo "$BASE" | sed -E 's#^[a-z]+://([^:/]+).*#\1#')
failed=0

check() { # description, then a command that succeeds when the check passes
    local what=$1; shift
    if "$@" >/dev/null 2>&1; then echo "PASS  $what"; else echo "FAIL  $what"; failed=1; fi
}
status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
is() { [ "$1" = "$2" ]; }
port_closed() { ! timeout 3 bash -c "</dev/tcp/$HOST/$1"; }

check "home page is served"                     is "$(status "$BASE/")" 200
check "a deep link (/police/records) gets the app" bash -c "curl -s '$BASE/police/records' | grep -q 'id=\"root\"'"
check "a missing file is a 404, not the app"    is "$(status "$BASE/assets/does-not-exist.js")" 404
check "security headers are set"                bash -c "curl -sI '$BASE/' | grep -qi 'x-content-type-options: nosniff'"
check "the API refuses a request without a key" is "$(status "$BASE/api/players")" 401
check "the admin key signs in"                  bash -c "curl -s -H 'X-API-Key: $KEY' '$BASE/api/whoami' | grep -q '\"admin\"'"
check "players come back from MySQL"            bash -c "curl -s -H 'X-API-Key: $KEY' '$BASE/api/players' | grep -q '\"name\"'"
check "lists are cached in Redis"               bash -c "curl -s -D - -o /dev/null -H 'X-API-Key: $KEY' '$BASE/api/warrants' >/dev/null; curl -s -D - -o /dev/null -H 'X-API-Key: $KEY' '$BASE/api/warrants' | grep -qi 'x-cache: hit'"

# Only the web port may be open to the outside.
check "MySQL (3306) is not exposed"             port_closed 3306
check "Redis (6379) is not exposed"             port_closed 6379
check "the load balancer (9000) is not exposed" port_closed 9000

# The audit log must record the visitor's real address, and an address
# made up in X-Forwarded-For must not be believed.
body='{"playerId":1,"charge":"smoke test","officer":"smoke","status":"pending"}'
check "a write with a forged X-Forwarded-For succeeds" is "$(status -X POST -H "X-API-Key: $KEY" -H 'Content-Type: application/json' -H 'X-Forwarded-For: 6.6.6.6' -d "$body" "$BASE/api/arrests")" 201
ip=$(curl -s -H "X-API-Key: $KEY" "$BASE/api/audit-log?limit=1" | sed -n 's/.*"ip":"\([^"]*\)".*/\1/p')
echo "      (audit log recorded the address $ip)"
check "the audit log ignored the forged address" bash -c "[ -n '$ip' ] && [ '$ip' != 6.6.6.6 ]"

exit $failed
