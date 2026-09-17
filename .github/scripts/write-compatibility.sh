#!/usr/bin/env bash
# Writes compatibility.yaml. The Kubernetes and Istio ranges track the pinned
# Istio's support window; the rest comes from versions.env. Env: TAG.
set -euo pipefail
: "${TAG:?}"
source scripts/cluster/versions.env
cat > compatibility.yaml <<YAML
release: ${TAG}
kubernetes:
  tested: ["1.31", "1.32", "1.33"]
istio:
  supported: ["1.29", "1.28"]
  mode: sidecar
  cni: required
spire:
  chart: "${SPIRE_CHART_VERSION}"
postgresql:
  supported: ["13", "14", "15", "16", "17"]
tooling:
  helm: "${HELM_VERSION}"
  helmfile: "${HELMFILE_VERSION}"
YAML
