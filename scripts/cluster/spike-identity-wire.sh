#!/usr/bin/env bash
# spike-identity-wire.sh — hand-wires option C3 (host-provided workload
# identity) from docs/adr/2026-07-07-ephemeral-development-environments-via-
# vcluster.md for the Phase 1 spike. Applies a standalone ClusterSPIFFEID
# (NOT a chart template — charts/orbit-platform is untouched) scoped to the
# spike's devenv namespace, and copies the host SPIRE trust bundle into the
# vcluster.
#
# Usage: scripts/cluster/spike-identity-wire.sh
# Requires: spike-vcluster-up.sh already run.
set -euo pipefail

HOST_NS=orbit-devenv-spike
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
TRUST_DOMAIN=orbit.test
CSPIFFEID_NAME=spire-system-spire-devenv-spike

if [ ! -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "vcluster kubeconfig not found at ${VCLUSTER_KUBECONFIG} — run spike-vcluster-up.sh first" >&2
  exit 1
fi

echo "==> applying spike ClusterSPIFFEID (host-side, hand-applied — mirrors charts/orbit-platform/templates/clusterSpiffeIDs.yaml conventions, not a chart change)"
cat <<EOF | kubectl apply -f -
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata:
  name: ${CSPIFFEID_NAME}
spec:
  className: spire-system-spire
  namespaceSelector:
    matchExpressions:
      - key: kubernetes.io/metadata.name
        operator: In
        values:
          - ${HOST_NS}
  podSelector:
    matchExpressions:
      - key: orbit.io/role
        operator: In
        values:
          - devenv-probe
  spiffeIDTemplate: "spiffe://${TRUST_DOMAIN}/devenv/spike/{{ .PodMeta.Name }}"
EOF

echo "==> spire-controller-manager status for the new ClusterSPIFFEID (entriesToSet should become >0 once probe pods exist)"
kubectl get clusterspiffeid "${CSPIFFEID_NAME}" -o jsonpath='{.status}' | { command -v jq >/dev/null && jq . || cat; }
echo

echo "==> copying host spire-bundle ConfigMap into the vcluster"
BUNDLE_FILE="${SPIKE_DIR}/bundle.spiffe"
kubectl get configmap spire-bundle -n spire-system -o jsonpath='{.data.bundle\.spiffe}' > "${BUNDLE_FILE}"
if [ ! -s "${BUNDLE_FILE}" ]; then
  echo "host spire-bundle ConfigMap has no bundle.spiffe data" >&2
  exit 1
fi

KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl create namespace spire-system --dry-run=client -o yaml \
  | KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl apply -f -
KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl create configmap spire-bundle -n spire-system \
  --from-file="bundle.spiffe=${BUNDLE_FILE}" \
  --dry-run=client -o yaml \
  | KUBECONFIG="${VCLUSTER_KUBECONFIG}" kubectl apply -f -

echo
echo "✓ identity wiring applied (ClusterSPIFFEID: ${CSPIFFEID_NAME}, bundle projected into vcluster spire-system)"
echo "Next: scripts/cluster/spike-istio-up.sh"
