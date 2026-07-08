#!/usr/bin/env bash
# spike-istio-up.sh — installs istio-base + istiod INSIDE the vcluster for
# the Phase 1 spike (docs/adr/2026-07-07-ephemeral-development-environments-
# via-vcluster.md). This is the single highest-uncertainty step the ADR
# flags: vcluster's official Istio integration only documents Ambient mode
# running on the host, not sidecar-mode istiod running inside a vcluster —
# this script is how we find out empirically whether it survives the syncer.
#
# Skips the `gateway` chart (not needed to answer any spike question) and
# the identity-proxy/opa-policy injection templates (irrelevant to proving
# SPIRE-socket + mTLS — only the `spire` template is needed).
#
# Usage: scripts/cluster/spike-istio-up.sh
# Requires: spike-vcluster-up.sh already run.
set -euo pipefail

SPIKE_DIR=/tmp/orbit-devenv-spike
VCLUSTER_KUBECONFIG="${SPIKE_DIR}/vcluster.kubeconfig"
ISTIO_VERSION=1.29.1
TRUST_DOMAIN=orbit.test
PROBE_NS=spike-probe

if [ ! -f "${VCLUSTER_KUBECONFIG}" ]; then
  echo "vcluster kubeconfig not found at ${VCLUSTER_KUBECONFIG} — run spike-vcluster-up.sh first" >&2
  exit 1
fi

export KUBECONFIG="${VCLUSTER_KUBECONFIG}"

echo "==> operating against vcluster context: $(kubectl config current-context)"

echo "==> installing istio-base ${ISTIO_VERSION}"
helm upgrade --install istio-base base \
  --repo https://istio-release.storage.googleapis.com/charts \
  --version "${ISTIO_VERSION}" \
  -n istio-system --create-namespace \
  --wait --timeout 5m

# sidecarInjectorWebhook.templates.spire is copied verbatim from
# buildSpireInjectionTemplate() in backend/internal/operator/istio_templates.go
# (lines 6-25) — this is Orbit's own opinionated injection wiring, not
# something the istiod chart provides. Kept in sync by hand for the spike;
# any drift between this file and istio_templates.go should be treated as a
# spike-script bug, not a real finding.
#
# pilot.env.ENABLE_NATIVE_SIDECARS=false is a REAL FINDING, not spike-only
# scaffolding: Istio auto-selects native-sidecar placement (istio-proxy as an
# initContainer with restartPolicy: Always) based on the API server's
# reported Kubernetes minor version. The vcluster's k8s distro reports a much
# newer version than the host, so istiod picks native-sidecar mode here —
# and buildSpireInjectionTemplate()/buildIdentityProxyInjectionTemplate() both
# hardcode a legacy `spec.containers: - name: istio-proxy` patch, which does
# NOT merge into a native-sidecar initContainer of the same name. Combining
# "sidecar,spire" without this override produces an invalid pod
# (spec.containers[1].image: Required value / duplicate istio-proxy). This
# is a latent gap in istio_templates.go, independent of vcluster: it would
# reproduce on any real cluster whose Kubernetes version crosses whatever
# threshold makes Istio 1.29.1 default to native sidecars.
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

echo "==> installing istiod ${ISTIO_VERSION}"
helm upgrade --install istiod istiod \
  --repo https://istio-release.storage.googleapis.com/charts \
  --version "${ISTIO_VERSION}" \
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
