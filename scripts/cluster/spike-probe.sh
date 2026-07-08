#!/usr/bin/env bash
# spike-probe.sh — non-interactive PASS/FAIL assertions for the Phase 1
# spike (docs/adr/2026-07-07-ephemeral-development-environments-via-
# vcluster.md). Deploys two pods inside the vcluster and checks, in order:
#   1. pods sync onto real host nodes
#   2. in-vcluster Istio sidecar injection survives the syncer
#   3. the SPIFFE CSI ephemeral inline volume is mounted and readable
#   4. host SPIRE attests the pod under spiffe://{trust-domain}/devenv/spike/...
#   5. pod-to-pod mTLS negotiates under STRICT PeerAuthentication
#   6. a NetworkPolicy fences the environment from the host control plane
#
# Usage: scripts/cluster/spike-probe.sh
# Requires: spike-vcluster-up.sh, spike-identity-wire.sh, spike-istio-up.sh
# already run.
set -euo pipefail

HOST_NS=orbit-devenv-spike
PROBE_NS=spike-probe
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
TRUST_DOMAIN=orbit.test

if [ ! -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "vcluster kubeconfig not found — run spike-vcluster-up.sh, spike-identity-wire.sh, spike-istio-up.sh first" >&2
  exit 1
fi

vc() { KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl "$@"; }

fails=0
pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails+1)); }

echo "==> deploying probe pods into vcluster namespace ${PROBE_NS}"
cat <<EOF | vc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: probe-server
  namespace: ${PROBE_NS}
  labels:
    orbit.io/role: devenv-probe
    app: probe-server
  annotations:
    inject.istio.io/templates: "sidecar,spire"
spec:
  containers:
    - name: app
      image: hashicorp/http-echo:1.0
      args: ["-text=probe-server", "-listen=:8080"]
      ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: probe-server
  namespace: ${PROBE_NS}
spec:
  selector:
    app: probe-server
  ports:
    - port: 8080
      targetPort: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: probe-client
  namespace: ${PROBE_NS}
  labels:
    orbit.io/role: devenv-probe
    app: probe-client
  annotations:
    inject.istio.io/templates: "sidecar,spire"
spec:
  containers:
    - name: app
      image: curlimages/curl:8.10.1
      command: ["sh", "-c", "sleep infinity"]
EOF

echo "==> applying STRICT PeerAuthentication in ${PROBE_NS} (forces mTLS)"
cat <<EOF | vc apply -f -
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: strict-mtls
  namespace: ${PROBE_NS}
spec:
  mtls:
    mode: STRICT
EOF

echo "==> waiting for probe pods to become Ready (includes sidecar readiness)"
vc wait --for=condition=Ready pod/probe-server -n "${PROBE_NS}" --timeout=180s || true
vc wait --for=condition=Ready pod/probe-client -n "${PROBE_NS}" --timeout=180s || true

# ── assertion 1: pods land as synced pods on a real host node ──────────────
echo
echo "assertion 1: pods synced onto real host nodes"
HOST_SERVER_POD="$(kubectl get pods -n "${HOST_NS}" -l orbit.io/role=devenv-probe,app=probe-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
HOST_CLIENT_POD="$(kubectl get pods -n "${HOST_NS}" -l orbit.io/role=devenv-probe,app=probe-client -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [ -n "${HOST_SERVER_POD}" ] && [ -n "${HOST_CLIENT_POD}" ]; then
  SERVER_NODE="$(kubectl get pod "${HOST_SERVER_POD}" -n "${HOST_NS}" -o jsonpath='{.spec.nodeName}')"
  pass "probe-server synced to host pod ${HOST_SERVER_POD} on node ${SERVER_NODE}"
else
  fail "could not find synced host pods for probe-server/probe-client in ${HOST_NS} (label orbit.io/role=devenv-probe)"
fi

# ── assertion 2: synced host pod has an injected istio-proxy container ─────
echo
echo "assertion 2: istio sidecar injection survives the syncer"
CONTAINERS=""
if [ -n "${HOST_SERVER_POD}" ]; then
  CONTAINERS="$(kubectl get pod "${HOST_SERVER_POD}" -n "${HOST_NS}" -o jsonpath='{.spec.containers[*].name}')"
  if echo "${CONTAINERS}" | grep -qw istio-proxy; then
    pass "host pod ${HOST_SERVER_POD} containers: ${CONTAINERS}"
  else
    fail "host pod ${HOST_SERVER_POD} has no istio-proxy container (containers: ${CONTAINERS}) — in-vcluster sidecar injection did NOT survive the sync"
  fi
else
  fail "skipped — no host pod found (assertion 1 failed)"
fi

# ── assertion 3: SPIFFE CSI ephemeral inline volume mounted & readable ─────
echo
echo "assertion 3: SPIFFE CSI ephemeral inline volume mounted in istio-proxy"
if [ -n "${HOST_SERVER_POD}" ] && echo "${CONTAINERS}" | grep -qw istio-proxy; then
  if kubectl exec "${HOST_SERVER_POD}" -n "${HOST_NS}" -c istio-proxy -- test -S /run/secrets/workload-spiffe-uds/socket 2>/dev/null; then
    pass "workload API socket present and is a socket file"
  else
    fail "/run/secrets/workload-spiffe-uds/socket missing or not a socket in istio-proxy container"
  fi
else
  fail "skipped — no istio-proxy container to check (assertion 2 failed)"
fi

# ── assertion 4: host SPIRE issues a devenv-prefixed SVID ──────────────────
echo
echo "assertion 4: host SPIRE attests the pod under spiffe://${TRUST_DOMAIN}/devenv/spike/..."
if [ -n "${HOST_SERVER_POD}" ] && echo "${CONTAINERS}" | grep -qw istio-proxy; then
  CERTS_JSON="$(kubectl exec "${HOST_SERVER_POD}" -n "${HOST_NS}" -c istio-proxy -- curl -s localhost:15000/certs 2>/dev/null || true)"
  if echo "${CERTS_JSON}" | grep -q "spiffe://${TRUST_DOMAIN}/devenv/spike/"; then
    pass "istio-proxy's own cert SAN contains a devenv/spike SVID"
  else
    fail "istio-proxy /certs did not contain a devenv/spike SVID URI (got: ${CERTS_JSON:0:300})"
  fi
else
  fail "skipped — no istio-proxy container to query (assertion 2 failed)"
fi

# ── assertion 5: mTLS negotiates pod-to-pod under STRICT mode ──────────────
#
# Requires a DestinationRule pinning subjectAltNames to the devenv-prefixed
# SVID identity-wire.sh causes SPIRE to issue. Root-caused after the initial
# spike run: without this, Envoy's automatic-mTLS SAN validation falls back
# to the standard spiffe://{trustDomain}/ns/{ns}/sa/{sa} convention and will
# never match a devenv/{env-id}/... SVID, so the handshake fails with
# CERTIFICATE_VERIFY_FAILED (surfaced to callers as a 503). This mirrors the
# pattern every chart-shipped service already uses — see
# charts/orbit/templates/identity/destination-rule.yaml. This is the fix, not
# scaffolding to throw away: the devenvironment reconciler (Phase 2) must
# template one of these per in-mesh environment service.
echo
echo "==> applying DestinationRule pinning the devenv-prefixed SVID as the expected server identity"
cat <<EOF | vc apply -f -
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: probe-server-devenv-mtls
  namespace: ${PROBE_NS}
spec:
  host: probe-server.${PROBE_NS}.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
      subjectAltNames:
        - "spiffe://${TRUST_DOMAIN}/devenv/spike/${HOST_SERVER_POD}"
EOF

echo
echo "assertion 5: pod-to-pod mTLS under STRICT PeerAuthentication"
CODE="$(vc exec probe-client -n "${PROBE_NS}" -c app -- curl -s --max-time 10 -o /dev/null -w '%{http_code}' "http://probe-server.${PROBE_NS}.svc.cluster.local:8080" 2>/dev/null || true)"
if [ "${CODE}" = "200" ]; then
  pass "probe-client reached probe-server over the mesh (STRICT mTLS required for this to succeed)"
else
  fail "probe-client could not reach probe-server through the mesh (http_code=${CODE:-none})"
fi

# ── assertion 6: NetworkPolicy fence (sync.toHost.networkPolicies) ─────────
echo
echo "assertion 6: NetworkPolicy fence — sync.toHost.networkPolicies + host isolation"
cat <<EOF | vc apply -f -
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-egress-except-dns
  namespace: ${PROBE_NS}
spec:
  podSelector: {}
  policyTypes: ["Egress"]
  egress:
    - to: []
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
EOF
# Sync from vcluster to host is asynchronous with substantial, variable
# latency — empirically observed anywhere from 15s to 70s+ in this
# environment across repeated runs. Poll generously rather than assume a
# fixed short sleep; this latency is itself a real finding (see the ADR),
# not just a script-timing nuisance.
for _ in $(seq 1 90); do
  kubectl get networkpolicy deny-egress-except-dns -n "${HOST_NS}" >/dev/null 2>&1 && break
  sleep 1
done
if kubectl get networkpolicy deny-egress-except-dns -n "${HOST_NS}" >/dev/null 2>&1; then
  pass "NetworkPolicy synced from vcluster to a real host object in ${HOST_NS} (sync.toHost.networkPolicies works)"
else
  fail "NetworkPolicy did NOT sync to a real host object in ${HOST_NS} — sync.toHost.networkPolicies is not behaving as expected"
fi

# Target by ClusterIP (resolved on the HOST) rather than DNS name, so a
# vcluster-side DNS-resolution gap can't be mistaken for the fence working.
IDENTITY_IP="$(kubectl get svc orbit-identity -n orbit-system -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
if [ -n "${IDENTITY_IP}" ]; then
  if vc exec probe-client -n "${PROBE_NS}" -c app -- curl -s --max-time 6 -o /dev/null "http://${IDENTITY_IP}:80" 2>/dev/null; then
    fail "probe-client REACHED host control-plane service orbit-identity.orbit-system (${IDENTITY_IP}:80) — NetworkPolicy fence is NOT holding"
  else
    pass "probe-client could NOT reach host control-plane service orbit-identity.orbit-system (${IDENTITY_IP}:80) — fence holds"
  fi
else
  fail "could not resolve orbit-identity ClusterIP on host to test the fence"
fi

echo
echo "spike-probe.sh: ${fails} failing assertion(s)"
exit "${fails}"
