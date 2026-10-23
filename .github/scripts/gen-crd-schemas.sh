#!/usr/bin/env bash
# Extract kora's own CRD OpenAPI schemas into per-kind JSON files so kubeconform
# validates kora custom resources in the rendered manifests. Third-party mesh
# CRDs (Istio, SPIRE) have no local schema and stay skipped via -ignore-missing-schemas.
# Output filenames match kubeconform's default -schema-location template:
#   <lowercase-kind>_<version>.json
set -euo pipefail

out="${1:?usage: gen-crd-schemas.sh <output-dir>}"
mkdir -p "${out}"

crds="${out}/_crds.yaml"
helm template orbit-crds charts/kora-crds -n orbit-system > "${crds}"

yq -N '
  select(.kind == "CustomResourceDefinition")
  | .spec.names.kind + " " + .spec.versions[].name
' "${crds}" | while read -r kind version; do
  lc="$(printf '%s' "${kind}" | tr '[:upper:]' '[:lower:]')"
  yq -N -o=json "
    select(.kind == \"CustomResourceDefinition\" and .spec.names.kind == \"${kind}\")
    | .spec.versions[] | select(.name == \"${version}\") | .schema.openAPIV3Schema
  " "${crds}" > "${out}/${lc}_${version}.json"
done

rm -f "${crds}"
