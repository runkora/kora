#!/usr/bin/env bash
# spike-istio-up.sh — installs istio-base + istiod INSIDE the vcluster.
# Ported (code_plan.md WP-15) as part of the permanent vcluster/Istio
# version-bump regression suite — see spike-vcluster-up.sh's header. This
# remains the single highest-uncertainty step across a version bump:
# vcluster's official Istio integration only documents Ambient mode running
# on the host, not sidecar-mode istiod running inside a vcluster — this
# script is how a VCLUSTER_CHART_VERSION or ISTIO_CHART_VERSION bump gets
# proven out empirically before it reaches the operator's own config.
#
# Skips the `gateway` chart (not needed to answer any regression question)
# and the identity-proxy/opa-policy injection templates (irrelevant to
# proving SPIRE-socket + mTLS — only the `spire` template is needed).
#
# Usage: scripts/cluster/spike-istio-up.sh
# Operates entirely against the vcluster's own kubeconfig (extracted by
# spike-vcluster-up.sh) — it never touches the host context directly, so
# ORBIT_CLOUD_KUBE_CONTEXT is not read here.
# Requires: spike-vcluster-up.sh and spike-identity-wire.sh already run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/cluster/versions.env
source "${SCRIPT_DIR}/versions.env"

SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
PROBE_NS=spike-probe

if [ ! -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "vcluster kubeconfig not found at ${VCLUSTER_KUBECONFIG} — run spike-vcluster-up.sh first" >&2
  exit 1
fi
if [ ! -f "${SPIKE_DIR}/trust-domain" ]; then
  echo "trust domain not recorded at ${SPIKE_DIR}/trust-domain — run spike-identity-wire.sh first" >&2
  exit 1
fi
TRUST_DOMAIN="$(cat "${SPIKE_DIR}/trust-domain")"

export KUBECONFIG="${VCLUSTER_KUBECONFIG}"

echo "==> operating against vcluster context: $(kubectl config current-context)"
echo "==> trust domain: ${TRUST_DOMAIN}"

echo "==> installing istio-base ${ISTIO_CHART_VERSION} (versions.env)"
helm upgrade --install istio-base base \
  --repo https://istio-release.storage.googleapis.com/charts \
  --version "${ISTIO_CHART_VERSION}" \
  -n istio-system --create-namespace \
  --wait --timeout 5m

# sidecarInjectorWebhook.templates.spire is copied verbatim from
# buildSpireInjectionTemplate() in backend/internal/operator/istio_templates.go
# — this is Orbit's own opinionated injection wiring, not something the
# istiod chart provides. Kept in sync by hand; any drift between this file
# and istio_templates.go should be treated as a regression-suite bug to fix,
# not a real finding.
#
# pilot.env.ENABLE_NATIVE_SIDECARS=false is a REAL, PERMANENT FINDING, not
# throwaway spike scaffolding: Istio auto-selects native-sidecar placement
# (istio-proxy as an initContainer with restartPolicy: Always) based on the
# API server's reported Kubernetes minor version. The vcluster's k8s distro
# reports a much newer version than most hosts, so istiod picks
# native-sidecar mode here — and buildSpireInjectionTemplate()/
# buildIdentityProxyInjectionTemplate() both hardcode a legacy
# `spec.containers: - name: istio-proxy` patch, which does NOT merge into a
# native-sidecar initContainer of the same name. Combining "sidecar,spire"
# without this override produces an invalid pod. This is what future
# ISTIO_CHART_VERSION bumps must re-verify: a newer Istio release could
# change the native-sidecar default threshold again.
cat > "${SPIKE_DIR}/istiod-values.yaml" <<EOF
meshConfig:
  trustDomain: ${TRUST_DOMAIN}
  defaultConfig:
    discoveryAddress: istiod.istio-system.svc:15012
pilot:
  env:
    ENABLE_NATIVE_SIDECARS: "false"
sidecarInjectorWebhook:
  templates:
    spire: |
      spec:
        containers:
          - name: istio-proxy
            env:
              - name: PILOT_CERT_PROVIDER
                value: "spire"
              - name: SPIFFE_ENDPOINT_SOCKET
                value: "unix:///run/secrets/workload-spiffe-uds/socket"
            volumeMounts:
              - name: workload-socket
                mountPath: /run/secrets/workload-spiffe-uds
                readOnly: true
        volumes:
          - name: workload-socket
            csi:
              driver: "csi.spiffe.io"
              readOnly: true
EOF

echo "==> installing istiod ${ISTIO_CHART_VERSION} (versions.env)"
helm upgrade --install istiod istiod \
  --repo https://istio-release.storage.googleapis.com/charts \
  --version "${ISTIO_CHART_VERSION}" \
  -n istio-system \
  -f "${SPIKE_DIR}/istiod-values.yaml" \
  --wait --timeout 5m

echo "==> waiting for istiod rollout"
kubectl rollout status deployment/istiod -n istio-system --timeout=180s

echo "==> creating ${PROBE_NS} namespace with istio-injection enabled"
kubectl create namespace "${PROBE_NS}" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "${PROBE_NS}" istio-injection=enabled --overwrite

echo
echo "✓ istio-base + istiod installed inside the vcluster"
echo "Next: scripts/cluster/spike-probe.sh"
