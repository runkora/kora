#!/usr/bin/env bash
# Writes compatibility.yaml. Istio minors come from the orbit chart's preflight
# list and the SPIRE chart from the reference install; tooling comes from
# versions.env. Keep the Kubernetes list in step with the charts CI render
# matrix. Env: TAG.
set -euo pipefail
: "${TAG:?}"
source scripts/cluster/versions.env
istio=$(yq -o=json -I=0 '.operator.preflight.supportedIstioMinors' charts/orbit/values.yaml)
spire=$(yq '.versions.spire' deploy/reference/environments/example.yaml)
cat > compatibility.yaml <<YAML
release: ${TAG}
kubernetes:
  tested: ["1.34", "1.35", "1.36"]
istio:
  supported: ${istio}
  mode: sidecar
  cni: required
spire:
  chart: "${spire}"
postgresql:
  supported: ["14", "15", "16", "17", "18"]
tooling:
  helm: "${HELM_VERSION}"
  helmfile: "${HELMFILE_VERSION}"
# Nodes, only for a deployment with a theme configured: the theme bundle is an
# image volume (docs/adr/2026-10-12-white-labelling-theme.md).
theming:
  kubernetes: ">=1.36"
  containerd: ">=2.1"
  crio: ">=1.31"
YAML
