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
#   --namespace <ns>      tenant namespace (default: kora-tenant-example)
#   --run <id>            run id; sandbox pod is sandbox-<id>
#   --pod <name>          sandbox pod name (overrides --run)
#   --blocked-host <h>    host NOT in the agent's posture (default: github.com)
#   --blocked-ip <ip>     IP for the raw-TCP probe (default: 1.1.1.1)
#   --allowed-host <h>    host IN the agent's posture; if set, assert it succeeds
set -euo pipefail

NS=kora-tenant-example
POD=""
RUN=""
BLOCKED_HOST=github.com
BLOCKED_IP=1.1.1.1
ALLOWED_HOST=""
# Gateway coordinates — the raw-TCP probe verifies denial by checking the
# gateway's audit log (see check 3), so it needs to know where the gateway runs.
GW_NS=kora-system
GW_DEPLOY=kora-egress-gateway
GW_CONTAINER=kora-egress-gateway

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace) NS="$2"; shift 2;;
    --run) RUN="$2"; shift 2;;
    --pod) POD="$2"; shift 2;;
    --blocked-host) BLOCKED_HOST="$2"; shift 2;;
    --blocked-ip) BLOCKED_IP="$2"; shift 2;;
    --allowed-host) ALLOWED_HOST="$2"; shift 2;;
    --gw-namespace) GW_NS="$2"; shift 2;;
    --gw-deploy) GW_DEPLOY="$2"; shift 2;;
    --gw-container) GW_CONTAINER="$2"; shift 2;;
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
# curl's OWN exit code is useless here: the sandbox runs a TRANSPARENT proxy, so
# the iptables REDIRECT always completes the local TCP handshake to Envoy before
# any policy is evaluated — curl reports "Connected to <ip>" and then a reset
# (rc=56) whether the IP was truly reached or Envoy denied it. A real bypass and
# a clean denial are indistinguishable from inside the sandbox.
#
# The authoritative oracle is the gateway: a relayed (i.e. bypassed) raw-TCP
# CONNECT emits an "egress relay opened host=<ip>" audit line (relay.go). A
# denial is dropped at the sandbox Envoy/OPA and never reaches the gateway, so
# no such line appears. We fire the probe, then check the gateway's recent log.
exec_sb "curl -s -o /dev/null --connect-timeout 7 http://$BLOCKED_IP:53/ >/dev/null 2>&1; true" >/dev/null || true
sleep 2
if kubectl logs -n "$GW_NS" "deploy/$GW_DEPLOY" -c "$GW_CONTAINER" --since=30s 2>/dev/null \
     | grep -i "relay opened" | grep -qF "$BLOCKED_IP"; then
  fail "raw TCP $BLOCKED_IP:53 was RELAYED by the gateway (bypass)"
else
  pass "raw TCP $BLOCKED_IP:53 denied (gateway never relayed it)"
fi

# 4) Optional: a whitelisted host must succeed THROUGH the tenant-CA gateway.
if [ -n "$ALLOWED_HOST" ]; then
  issuer="$(exec_sb "curl -sv --max-time 12 https://$ALLOWED_HOST -o /dev/null 2>&1 | grep -i 'issuer:' || true")"
  if printf '%s' "$issuer" | grep -qi "kora Tenant CA"; then
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
