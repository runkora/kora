#!/usr/bin/env bash
# spike-teardown.sh — idempotent cleanup for the Phase 1 vcluster spike
# (docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
# Safe to re-run; every step tolerates "already gone".
#
# Usage: scripts/cluster/spike-teardown.sh
set -uo pipefail

HOST_NS=orbit-devenv-spike
RELEASE=vcluster-spike
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
CSPIFFEID_NAME=spire-system-spire-devenv-spike

if [ -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "==> uninstalling in-vcluster istio releases"
  KUBECONFIG="${VCLUSTER_KUBECONFIG}" helm uninstall istiod -n istio-system >/dev/null 2>&1 || true
  KUBECONFIG="${VCLUSTER_KUBECONFIG}" helm uninstall istio-base -n istio-system >/dev/null 2>&1 || true
fi

PF_PID_FILE="${SPIKE_DIR}/port-forward.pid"
if [ -f "${PF_PID_FILE}" ]; then
  echo "==> stopping vcluster port-forward"
  kill "$(cat "${PF_PID_FILE}")" 2>/dev/null || true
fi

echo "==> uninstalling vcluster release ${RELEASE}"
helm uninstall "${RELEASE}" -n "${HOST_NS}" >/dev/null 2>&1 || true

echo "==> deleting host namespace ${HOST_NS} (this also removes anything vcluster synced there)"
kubectl delete namespace "${HOST_NS}" --ignore-not-found --wait=false

echo "==> deleting spike ClusterSPIFFEID"
kubectl delete clusterspiffeid "${CSPIFFEID_NAME}" --ignore-not-found

echo "==> removing local scratch files (${SPIKE_DIR})"
rm -rf "${SPIKE_DIR}"

echo
echo "✓ teardown complete (namespace deletion continues in the background — check with: kubectl get ns ${HOST_NS})"
