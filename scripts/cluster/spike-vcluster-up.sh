#!/usr/bin/env bash
# spike-vcluster-up.sh — Phase 1 spike for docs/adr/2026-07-07-ephemeral-
# development-environments-via-vcluster.md. Stands up a single vcluster on
# the local k3d host to answer the ADR's Phase 1 spike questions. This is
# hand-wired scaffolding for the spike ONLY — no OrbitDevEnvironment CRD, no
# operator reconciler, no chart changes. See spike-teardown.sh to tear down.
#
# Usage: scripts/cluster/spike-vcluster-up.sh
#
# Never touches the caller's default kubeconfig/context: the vcluster
# kubeconfig is written to a separate file under SPIKE_DIR.
set -euo pipefail

HOST_NS=orbit-devenv-spike
RELEASE=vcluster-spike
VCLUSTER_VERSION=0.35.1
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"

mkdir -p "${SPIKE_DIR}"

echo "==> host context: $(kubectl config current-context)"

echo "==> creating namespace ${HOST_NS}"
kubectl create namespace "${HOST_NS}" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "${HOST_NS}" orbit.io/role=devenv --overwrite

echo "==> adding/updating loft-sh helm repo"
helm repo add loft-sh https://charts.loft.sh >/dev/null 2>&1 || true
helm repo update loft-sh >/dev/null

# vcluster's k3s distro was removed in v0.33 — only the k8s distro remains,
# and it is NOT enabled by default (controlPlane.distro.k8s.enabled: false
# in the chart's own values). Must be set explicitly.
#
# sync.toHost.serviceAccounts and sync.toHost.networkPolicies default to
# false; both are required for this ADR's C3 identity model (SPIRE's
# k8s_psat workload attestor needs a real host ServiceAccount object) and
# NetworkPolicy host-fencing claim to work at all. sync.fromHost.csiDrivers
# defaults to "auto" (enabled when the virtual scheduler is on) — set
# explicitly to true rather than relying on auto-detection.
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

echo "==> installing vcluster ${VCLUSTER_VERSION} into ${HOST_NS}"
helm upgrade --install "${RELEASE}" vcluster \
  --repo https://charts.loft.sh \
  --version "${VCLUSTER_VERSION}" \
  -n "${HOST_NS}" \
  -f "${SPIKE_DIR}/vcluster-values.yaml" \
  --wait --timeout 5m

echo "==> waiting for vcluster control plane pod"
kubectl wait --for=condition=Ready pod -l app=vcluster -n "${HOST_NS}" --timeout=180s

echo "==> extracting vcluster kubeconfig to ${VCLUSTER_KUBECONFIG}"
# vc-config-NAME (note: NOT the kubeconfig — it's the rendered vcluster.yaml
# values, keyed "config.yaml") is a DIFFERENT secret from vc-NAME (the actual
# kubeconfig, keyed "config"). Both match a loose "vc-*NAME" pattern, so
# match exactly rather than substring.
SECRET_NAME="vc-${RELEASE}"
if ! kubectl get secret "${SECRET_NAME}" -n "${HOST_NS}" >/dev/null 2>&1; then
  echo "expected secret ${SECRET_NAME} not found — listing secrets in ${HOST_NS} for debugging:" >&2
  kubectl get secrets -n "${HOST_NS}" >&2
  exit 1
fi
kubectl get secret "${SECRET_NAME}" -n "${HOST_NS}" -o jsonpath='{.data.config}' | base64 -d > "${VCLUSTER_KUBECONFIG}"

# The generated kubeconfig's server defaults to https://localhost:8443, which
# normally only works because `vcluster connect` runs a background
# `kubectl port-forward` for you. We're driving vcluster via its Helm chart
# directly (no vcluster CLI installed — see the ADR's own note), so start
# and track that port-forward ourselves.
PF_LOG="${SPIKE_DIR}/port-forward.log"
PF_PID_FILE="${SPIKE_DIR}/port-forward.pid"
if [ -f "${PF_PID_FILE}" ] && kill -0 "$(cat "${PF_PID_FILE}")" 2>/dev/null; then
  echo "==> port-forward already running (pid $(cat "${PF_PID_FILE}"))"
else
  echo "==> starting background port-forward to svc/${RELEASE} (127.0.0.1:8443)"
  nohup kubectl port-forward -n "${HOST_NS}" "svc/${RELEASE}" 8443:443 > "${PF_LOG}" 2>&1 &
  disown
  echo $! > "${PF_PID_FILE}"
  sleep 3
fi

HOST_CONTEXT="$(kubectl config current-context)"
if ! VCLUSTER_CONTEXT="$(KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl config current-context 2>&1)" || \
   ! KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl get ns >/dev/null 2>&1; then
  echo "vcluster kubeconfig is not reachable through the port-forward — check ${PF_LOG}" >&2
  exit 1
fi

echo
echo "✓ vcluster up"
echo "  host context:     ${HOST_CONTEXT}"
echo "  vcluster context: ${VCLUSTER_CONTEXT} (kubeconfig: ${VCLUSTER_KUBECONFIG})"
echo "  port-forward:     pid $(cat "${PF_PID_FILE}"), log at ${PF_LOG}"
echo
echo "Use: KUBECONFIG=${VCLUSTER_KUBECONFIG} kubectl get ns   # to talk to the vcluster"
echo "Next: scripts/cluster/spike-identity-wire.sh"
