#!/usr/bin/env bash
# spike-vcluster-up.sh — stands up a single vcluster on the cloud staging
# host. Originally a Phase 1 spike for docs/adr/2026-07-07-ephemeral-
# development-environments-via-vcluster.md; now ported (code_plan.md WP-15)
# as the permanent vcluster/Istio version-bump regression suite: run this
# whenever VCLUSTER_CHART_VERSION or ISTIO_CHART_VERSION in versions.env
# changes, before rolling the bump out to the real operator config.
#
# This is hand-wired scaffolding, independent of the operator: no
# OrbitDevEnvironment CRD, no reconciler, no chart changes exercised here —
# it validates the underlying vcluster+Istio+SPIRE combination in isolation
# so a version bump's blast radius is understood before the operator ever
# touches it.
#
# Usage: ORBIT_CLOUD_KUBE_CONTEXT=<staging-context> scripts/cluster/spike-vcluster-up.sh
#
# Never touches any other context: every kubectl/helm call below is pinned
# to ORBIT_CLOUD_KUBE_CONTEXT explicitly (never kubectl's ambient
# current-context) — this suite targets the cloud staging host only, never
# k3d, and a forgotten context switch must never silently hit the wrong
# cluster. The vcluster's own kubeconfig is written to a separate file under
# SPIKE_DIR and is never merged into the caller's default kubeconfig.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/cluster/versions.env
source "${SCRIPT_DIR}/versions.env"

: "${ORBIT_CLOUD_KUBE_CONTEXT:?ORBIT_CLOUD_KUBE_CONTEXT must be set to the staging clusters kubectl context name}"

HOST_NS=orbit-devenv-vcluster-regression
RELEASE=vcluster-regression
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"

kx() { kubectl --context "${ORBIT_CLOUD_KUBE_CONTEXT}" "$@"; }
hx() { helm --kube-context "${ORBIT_CLOUD_KUBE_CONTEXT}" "$@"; }

mkdir -p "${SPIKE_DIR}"

echo "==> host context: ${ORBIT_CLOUD_KUBE_CONTEXT}"

echo "==> creating namespace ${HOST_NS}"
kx create namespace "${HOST_NS}" --dry-run=client -o yaml | kx apply -f -
kx label namespace "${HOST_NS}" orbit.io/role=devenv --overwrite

echo "==> adding/updating loft-sh helm repo"
helm repo add loft-sh https://charts.loft.sh >/dev/null 2>&1 || true
helm repo update loft-sh >/dev/null

# vcluster's k3s distro was removed in v0.33 — only the k8s distro remains,
# and it is NOT enabled by default (controlPlane.distro.k8s.enabled: false
# in the chart's own values). Must be set explicitly.
#
# sync.toHost.serviceAccounts and sync.toHost.networkPolicies default to
# false; both are required for the ADR's C3 identity model (SPIRE's
# k8s_psat workload attestor needs a real host ServiceAccount object) and
# NetworkPolicy host-fencing claim to work at all. sync.fromHost.csiDrivers
# defaults to "auto" (enabled when the virtual scheduler is on) — set
# explicitly to true rather than relying on auto-detection. These three
# values are pinned decision 1 — the operator's own ensureVCluster keeps
# them in sync (backend/internal/operator/devenvironment_platform.go).
cat > "${SPIKE_DIR}/vcluster-values.yaml" <<'EOF'
controlPlane:
  distro:
    k8s:
      enabled: true
sync:
  toHost:
    serviceAccounts:
      enabled: true
    networkPolicies:
      enabled: true
  fromHost:
    csiDrivers:
      enabled: true
EOF

echo "==> installing vcluster ${VCLUSTER_CHART_VERSION} (versions.env) into ${HOST_NS}"
hx upgrade --install "${RELEASE}" vcluster \
  --repo https://charts.loft.sh \
  --version "${VCLUSTER_CHART_VERSION}" \
  -n "${HOST_NS}" \
  -f "${SPIKE_DIR}/vcluster-values.yaml" \
  --wait --timeout 5m

echo "==> waiting for vcluster control plane pod"
kx wait --for=condition=Ready pod -l app=vcluster -n "${HOST_NS}" --timeout=180s

echo "==> extracting vcluster kubeconfig to ${VCLUSTER_KUBECONFIG}"
# vc-config-NAME (note: NOT the kubeconfig — it's the rendered vcluster.yaml
# values, keyed "config.yaml") is a DIFFERENT secret from vc-NAME (the actual
# kubeconfig, keyed "config"). Both match a loose "vc-*NAME" pattern, so
# match exactly rather than substring.
SECRET_NAME="vc-${RELEASE}"
if ! kx get secret "${SECRET_NAME}" -n "${HOST_NS}" >/dev/null 2>&1; then
  echo "expected secret ${SECRET_NAME} not found — listing secrets in ${HOST_NS} for debugging:" >&2
  kx get secrets -n "${HOST_NS}" >&2
  exit 1
fi
kx get secret "${SECRET_NAME}" -n "${HOST_NS}" -o jsonpath='{.data.config}' | base64 -d > "${VCLUSTER_KUBECONFIG}"

# The generated kubeconfig's server defaults to https://localhost:8443, which
# normally only works because `vcluster connect` runs a background
# `kubectl port-forward` for you. We're driving vcluster via its Helm chart
# directly (no vcluster CLI installed), so start and track that
# port-forward ourselves — pinned to the same context via kx.
PF_LOG="${SPIKE_DIR}/port-forward.log"
PF_PID_FILE="${SPIKE_DIR}/port-forward.pid"
if [ -f "${PF_PID_FILE}" ] && kill -0 "$(cat "${PF_PID_FILE}")" 2>/dev/null; then
  echo "==> port-forward already running (pid $(cat "${PF_PID_FILE}"))"
else
  echo "==> starting background port-forward to svc/${RELEASE} (127.0.0.1:8443)"
  nohup kubectl --context "${ORBIT_CLOUD_KUBE_CONTEXT}" port-forward -n "${HOST_NS}" "svc/${RELEASE}" 8443:443 \
    > "${PF_LOG}" 2>&1 &
  disown
  echo $! > "${PF_PID_FILE}"
  sleep 3
fi

if ! VCLUSTER_CONTEXT="$(KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl config current-context 2>&1)" || \
   ! KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl get ns >/dev/null 2>&1; then
  echo "vcluster kubeconfig is not reachable through the port-forward — check ${PF_LOG}" >&2
  exit 1
fi

echo
echo "✓ vcluster up"
echo "  host context:     ${ORBIT_CLOUD_KUBE_CONTEXT}"
echo "  vcluster context: ${VCLUSTER_CONTEXT} (kubeconfig: ${VCLUSTER_KUBECONFIG})"
echo "  port-forward:     pid $(cat "${PF_PID_FILE}"), log at ${PF_LOG}"
echo
echo "Use: KUBECONFIG=${VCLUSTER_KUBECONFIG} kubectl get ns   # to talk to the vcluster"
echo "Next: ORBIT_CLOUD_KUBE_CONTEXT=${ORBIT_CLOUD_KUBE_CONTEXT} scripts/cluster/spike-identity-wire.sh"
