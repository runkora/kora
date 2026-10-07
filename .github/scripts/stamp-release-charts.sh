#!/usr/bin/env bash
# Stamp both charts to the release version and pin every orbit chart image to a
# digest. Resolves digests on a working copy and fails on any empty result, so a
# partial or empty pin never reaches the packaged chart. Writes images.txt: the
# chart's images plus every image docker-bake.hcl publishes, so a new bake
# target is listed and signed without touching this script.
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

digest_of() {
  local digest
  digest=$(docker buildx imagetools inspect "$1" --format '{{println .Manifest.Digest}}' | head -1)
  [ -n "${digest}" ] || { echo "::error::no digest resolved for $1" >&2; exit 1; }
  echo "${digest}"
}

for key in ${components}; do
  registry=$(yq -r ".${key}.image.registry // \"\"" "${work}")
  repo=$(yq -r ".${key}.image.repository" "${work}")
  reg="${registry:-${REGISTRY}/${IMAGE_NAMESPACE}}"
  tag=$(yq -r ".${key}.image.tag // \"\"" "${work}")
  digest=$(digest_of "${reg}/${repo}:${tag:-${TAG}}")
  yq -i ".${key}.image.digest = \"${digest}\"" "${work}"
  echo "${reg}/${repo}@${digest}" >> images.txt
done

# orbit-agents runs the session-manager image in tenant namespaces, so the chart
# references it through agents.config rather than a component image block.
digest=$(digest_of "${REGISTRY}/${IMAGE_NAMESPACE}/orbit-session-manager:${TAG}")
yq -i ".agents.config.sessionManagerDigest = \"${digest}\"" "${work}"

published=$(IMAGE_TAG="${TAG}" docker buildx bake --print -f docker-bake.hcl \
  | jq -r '.target[].tags[]' | sort -u)
for ref in ${published}; do
  digest=$(digest_of "${ref}")
  echo "${ref%:*}@${digest}" >> images.txt
done

sort -u images.txt -o images.txt
[ -s images.txt ] || { echo "::error::no chart images resolved from values.yaml" >&2; exit 1; }
mv "${work}" charts/orbit/values.yaml
