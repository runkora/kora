#!/usr/bin/env bash
# =============================================================================
# register-k8s-connection.sh — Register the local cluster as a Kubernetes
# connection for the example tenant.
#
# Usage:
#   bash register-k8s-connection.sh [namespace]   # default: orbit-tenant-example
#
# Creates a ServiceAccount whose RBAC is deliberately narrow, mints a bound
# token for it, and applies a Connection CR pointing at the in-cluster API
# server. The agent authenticates as this ServiceAccount, so the cluster's own
# RBAC is the outer ceiling on anything Orbit policy allows through.
#
# baseHost is the k3d server's own hostname, which CoreDNS resolves from its
# NodeHosts entries and which is on the API server certificate. It is
# deliberately not kubernetes.default.svc.cluster.local: that is a mesh
# service, so the sandbox's sidecar routes it inside the mesh and the traffic
# never reaches the egress chokepoint. A bare IP is rejected too, since TLS to
# an IP literal carries no SNI for the interceptor to match.
#
# Idempotent: safe to re-run. The token is reissued on every run.
# =============================================================================
set -euo pipefail

NAMESPACE="${1:-orbit-tenant-example}"
SA_NAME="orbit-egress-local"
SECRET_NAME="orbit-local-token"
# The role and binding are cluster-scoped, so their names carry the namespace.
# Without it, registering a second tenant would rebind the role to that tenant's
# ServiceAccount and silently revoke the first.
ROLE_NAME="orbit-egress-local-reader-${NAMESPACE}"
TOKEN_TTL="24h"
API_HOST="k3d-orbit-local-server-0:6443"

echo "Registering the local Kubernetes connection in ${NAMESPACE}"

# The ServiceAccount the agent acts as. Read-only, and no access to Secrets:
# the connector's whole point is that the cluster bounds what policy permits.
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SA_NAME}
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${ROLE_NAME}
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "configmaps", "namespaces", "nodes", "events"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${ROLE_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${ROLE_NAME}
subjects:
  - kind: ServiceAccount
    name: ${SA_NAME}
    namespace: ${NAMESPACE}
EOF

echo "Minting a ${TOKEN_TTL} token for ${SA_NAME}"
TOKEN="$(kubectl create token "${SA_NAME}" -n "${NAMESPACE}" --duration="${TOKEN_TTL}")"

kubectl create secret generic "${SECRET_NAME}" \
  -n "${NAMESPACE}" \
  --from-literal=token="${TOKEN}" \
  --dry-run=client -o yaml | kubectl apply -f -

# The gateway verifies the API server against this CA. It is cluster-specific,
# so it is read live rather than committed. caCert is a connection field, not
# adapter config: the gateway owns the outbound TLS leg.
CA_CERT="$(kubectl config view --raw --minify \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d)"

if [ -z "${CA_CERT}" ]; then
  echo "✗ no certificate-authority-data in the current kubeconfig context" >&2
  exit 1
fi

echo "Applying the Connection resource"
{
  cat <<EOF
apiVersion: orbit.io/v1alpha1
kind: Connection
metadata:
  name: orbit-local
  namespace: ${NAMESPACE}
spec:
  adapter: kubernetes
  baseHost: ${API_HOST}
  secretRef: ${SECRET_NAME}
  secretKey: token
  caCert: |
EOF
  echo "${CA_CERT}" | sed 's/^/    /'
} | kubectl apply -f -

echo "✓ Kubernetes connection registered as 'orbit-local' in ${NAMESPACE}"
echo "  Grant an agent connection:orbit-local:workloads:read to use it."
