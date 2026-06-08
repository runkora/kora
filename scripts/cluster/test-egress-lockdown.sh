#!/usr/bin/env bash
# test-egress-lockdown.sh — assert the transparent sandbox egress chokepoint.
#
# The sandbox Envoy rewrites ALL outbound TCP into a CONNECT to the per-tenant
# egress gateway, gated by OPA posture (docs/adr/2026-06-06-jit-credential-
# brokering-egress-gateway.md, Amendment 2). This script proves a sandbox cannot
# reach a non-whitelisted host on ANY port — the bypass that previously leaked —
# and (optionally) that a whitelisted host succeeds THROUGH the tenant-CA gateway.
#
# It runs against an ALREADY-PROVISIONED sandbox (provision a run with
# {"provision_sandbox": true}); it does not create runs itself.
#
# Usage:
#   scripts/cluster/test-egress-lockdown.sh --run <run-id> [opts]
#   scripts/cluster/test-egress-lockdown.sh --pod sandbox-<id> [opts]
#
# Options:
#   --namespace <ns>      tenant namespace (default: orbit-tenant-example)
#   --run <id>            run id; sandbox pod is sandbox-<id>
#   --pod <name>          sandbox pod name (overrides --run)
#   --blocked-host <h>    host NOT in the agent's posture (default: github.com)
#   --blocked-ip <ip>     IP for the raw-TCP probe (default: 1.1.1.1)
#   --allowed-host <h>    host IN the agent's posture; if set, assert it succeeds
set -euo pipefail

NS=orbit-tenant-example
POD=""
RUN=""
BLOCKED_HOST=github.com
BLOCKED_IP=1.1.1.1
ALLOWED_HOST=""

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace) NS="$2"; shift 2;;
    --run) RUN="$2"; shift 2;;
    --pod) POD="$2"; shift 2;;
    --blocked-host) BLOCKED_HOST="$2"; shift 2;;
    --blocked-ip) BLOCKED_IP="$2"; shift 2;;
    --allowed-host) ALLOWED_HOST="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[ -z "$POD" ] && [ -n "$RUN" ] && POD="sandbox-$RUN"
if [ -z "$POD" ]; then echo "error: --run or --pod required" >&2; exit 2; fi

exec_sb() { kubectl exec -n "$NS" "$POD" -c sandbox-exec -- sh -c "$1" 2>/dev/null; }

fails=0
pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails+1)); }

echo "egress lockdown checks against $NS/$POD"

# 1) HTTPS to a non-whitelisted host must be denied (SNI posture).
if exec_sb "curl -s --max-time 8 https://$BLOCKED_HOST -o /dev/null" ; then
  fail "https://$BLOCKED_HOST was REACHED (bypass)"
else
  pass "https://$BLOCKED_HOST denied"
fi

# 2) Plain HTTP (port 80) to a non-whitelisted host must be denied (OPA on HCM).
code="$(exec_sb "curl -s --max-time 8 -o /dev/null -w '%{http_code}' http://$BLOCKED_HOST" || true)"
if [ "$code" = "200" ] || [ "$code" = "301" ] || [ "$code" = "302" ]; then
  fail "http://$BLOCKED_HOST returned $code (bypass)"
else
  pass "http://$BLOCKED_HOST denied (code=$code)"
fi

# 3) Arbitrary TCP to a non-whitelisted IP must be denied (catch-all/IP posture).
if exec_sb "timeout 7 curl -s http://$BLOCKED_IP:53/ -o /dev/null" ; then
  fail "raw TCP $BLOCKED_IP:53 was REACHED (bypass)"
else
  pass "raw TCP $BLOCKED_IP:53 denied"
fi

# 4) Optional: a whitelisted host must succeed THROUGH the tenant-CA gateway.
if [ -n "$ALLOWED_HOST" ]; then
  issuer="$(exec_sb "curl -sv --max-time 12 https://$ALLOWED_HOST -o /dev/null 2>&1 | grep -i 'issuer:' || true")"
  if printf '%s' "$issuer" | grep -qi "Orbit Tenant CA"; then
    pass "https://$ALLOWED_HOST allowed via tenant-CA gateway"
  else
    fail "https://$ALLOWED_HOST did not present a tenant-CA leaf (issuer: ${issuer:-none})"
  fi
fi

echo
if [ "$fails" -eq 0 ]; then
  echo "✓ egress lockdown holds"
else
  echo "✗ $fails check(s) failed — egress is NOT locked down"
fi
exit "$fails"
