#!/usr/bin/env bash
# spike-identity-wire.sh — hand-wires option C3 (host-provided workload
# identity) from docs/adr/2026-07-07-ephemeral-development-environments-via-
# vcluster.md. Applies a standalone ClusterSPIFFEID (NOT a chart template —
# charts/orbit-platform is untouched) scoped to this suite's devenv
# namespace, and copies the host SPIRE trust bundle into the vcluster.
#
# Ported (code_plan.md WP-15) as part of the permanent vcluster/Istio
# version-bump regression suite — see spike-vcluster-up.sh's header.
#
# Usage: ORBIT_CLOUD_KUBE_CONTEXT=<staging-context> scripts/cluster/spike-identity-wire.sh
# Requires: spike-vcluster-up.sh already run against the same context.
set -euo pipefail

: "${ORBIT_CLOUD_KUBE_CONTEXT:?ORBIT_CLOUD_KUBE_CONTEXT must be set to the staging clusters kubectl context name}"

HOST_NS=orbit-devenv-vcluster-regression
SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
CSPIFFEID_NAME=spire-system-spire-devenv-regression

kx() { kubectl --context "${ORBIT_CLOUD_KUBE_CONTEXT}" "$@"; }

if [ ! -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "vcluster kubeconfig not found at ${VCLUSTER_KUBECONFIG} — run spike-vcluster-up.sh first" >&2
  exit 1
fi

# The trust domain is read from the host's own OrbitPlatform CR, never
# configured per environment or hardcoded here (Track V item 7 /
# backend/internal/operator/devenvironment_platform.go's hostTrustDomain) —
# a spike hardcoding a fake trust domain would validate a scenario the real
# reconciler never runs.
TRUST_DOMAIN="$(kx get orbitplatforms -o jsonpath='{.items[0].spec.trustDomain}')"
if [ -z "${TRUST_DOMAIN}" ]; then
  echo "could not read spec.trustDomain from any host OrbitPlatform — is Track V's platform install complete?" >&2
  exit 1
fi
echo "==> using host trust domain: ${TRUST_DOMAIN}"
echo -n "${TRUST_DOMAIN}" > "${SPIKE_DIR}/trust-domain"

echo "==> applying regression-suite ClusterSPIFFEID (host-side, hand-applied — mirrors charts/orbit-platform/templates/clusterSpiffeIDs.yaml conventions, not a chart change)"
cat <<EOF | kx apply -f -
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
  spiffeIDTemplate: "spiffe://${TRUST_DOMAIN}/devenv/regression/{{ .PodMeta.Name }}"
EOF

echo "==> spire-controller-manager status for the new ClusterSPIFFEID (entriesToSet should become >0 once probe pods exist)"
kx get clusterspiffeid "${CSPIFFEID_NAME}" -o jsonpath='{.status}' | { command -v jq >/dev/null && jq . || cat; }
echo

echo "==> copying host spire-bundle ConfigMap into the vcluster"
BUNDLE_FILE="${SPIKE_DIR}/bundle.spiffe"
kx get configmap spire-bundle -n spire-system -o jsonpath='{.data.bundle\.spiffe}' > "${BUNDLE_FILE}"
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
echo "✓ identity wiring applied (ClusterSPIFFEID: ${CSPIFFEID_NAME}, trust domain: ${TRUST_DOMAIN}, bundle projected into vcluster spire-system)"
echo "Next: ORBIT_CLOUD_KUBE_CONTEXT=${ORBIT_CLOUD_KUBE_CONTEXT} scripts/cluster/spike-istio-up.sh"
