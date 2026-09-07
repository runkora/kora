#!/usr/bin/env bash
# Chart digests come from the helm push logs in push-<chart>.txt; image refs
# from images.txt. Env: REGISTRY, IMAGE_NAMESPACE, COSIGN_KEY, COSIGN_PASSWORD.
set -euo pipefail
: "${REGISTRY:?}" "${IMAGE_NAMESPACE:?}" "${COSIGN_KEY:?}"

for chart in orbit-crds orbit; do
  digest=$(grep -oE 'sha256:[0-9a-f]{64}' "push-${chart}.txt" | head -1)
  [ -n "${digest}" ] || { echo "::error::no chart digest in push-${chart}.txt" >&2; exit 1; }
  cosign sign --yes --key env://COSIGN_KEY "${REGISTRY}/${IMAGE_NAMESPACE}/charts/${chart}@${digest}"
done

while read -r ref; do
  cosign sign --yes --key env://COSIGN_KEY "${ref}"
done < images.txt
