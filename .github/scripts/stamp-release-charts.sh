#!/usr/bin/env bash
# Stamp both charts to the release version and pin every orbit chart image to a
# digest. Resolves digests on a working copy and fails on any empty result, so a
# partial or empty pin never reaches the packaged chart. Writes images.txt.
#
# Env: TAG, VERSION, REGISTRY, IMAGE_NAMESPACE.
set -euo pipefail

: "${TAG:?}" "${VERSION:?}" "${REGISTRY:?}" "${IMAGE_NAMESPACE:?}"

.github/scripts/build-chart-deps.sh

for chart in orbit orbit-crds; do
  yq -i ".version = \"${VERSION}\" | .appVersion = \"${TAG}\"" "charts/${chart}/Chart.yaml"
done

work=$(mktemp)
cp charts/orbit/values.yaml "${work}"
: > images.txt

components=$(yq -o=json '.' "${work}" \
  | jq -r 'to_entries[] | select((.value | type) == "object" and (.value.image.repository? // "") != "") | .key')

for key in ${components}; do
  registry=$(yq -r ".${key}.image.registry // \"\"" "${work}")
  repo=$(yq -r ".${key}.image.repository" "${work}")
  reg="${registry:-${REGISTRY}/${IMAGE_NAMESPACE}}"
  tag=$(yq -r ".${key}.image.tag // \"\"" "${work}")
  ref="${reg}/${repo}:${tag:-${TAG}}"
  digest=$(docker buildx imagetools inspect "${ref}" --format '{{println .Manifest.Digest}}' | head -1)
  [ -n "${digest}" ] || { echo "::error::no digest resolved for ${ref}" >&2; exit 1; }
  yq -i ".${key}.image.digest = \"${digest}\"" "${work}"
  echo "${reg}/${repo}@${digest}" >> images.txt
done

sort -u images.txt -o images.txt
[ -s images.txt ] || { echo "::error::no chart images resolved from values.yaml" >&2; exit 1; }
mv "${work}" charts/orbit/values.yaml
