#!/usr/bin/env bash
# spike-teardown.sh — idempotent cleanup for the vcluster/Istio version-bump
# regression suite (ported from the Phase 1 spike, code_plan.md WP-15 — see
# spike-vcluster-up.sh's header). Safe to re-run; every step tolerates
# "already gone".
#
# Usage: ORBIT_CLOUD_KUBE_CONTEXT=<staging-context> scripts/cluster/spike-teardown.sh
set -uo pipefail

: "${ORBIT_CLOUD_KUBE_CONTEXT:?ORBIT_CLOUD_KUBE_CONTEXT must be set to the staging clusters kubectl context name}"

HOST_NS=orbit-devenv-vcluster-regression
RELEASE=vcluster-regression
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
CSPIFFEID_NAME=spire-system-spire-devenv-regression

kx() { kubectl --context "${ORBIT_CLOUD_KUBE_CONTEXT}" "$@"; }
hx() { helm --kube-context "${ORBIT_CLOUD_KUBE_CONTEXT}" "$@"; }

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
hx uninstall "${RELEASE}" -n "${HOST_NS}" >/dev/null 2>&1 || true

echo "==> deleting host namespace ${HOST_NS} (this also removes anything vcluster synced there)"
kx delete namespace "${HOST_NS}" --ignore-not-found --wait=false

echo "==> deleting regression-suite ClusterSPIFFEID"
kx delete clusterspiffeid "${CSPIFFEID_NAME}" --ignore-not-found

echo "==> removing local scratch files (${SPIKE_DIR})"
rm -rf "${SPIKE_DIR}"

echo
echo "✓ teardown complete (namespace deletion continues in the background — check with: kubectl --context ${ORBIT_CLOUD_KUBE_CONTEXT} get ns ${HOST_NS})"
