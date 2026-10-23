#!/usr/bin/env bash
# Chart digests come from the helm push logs in push-<chart>.txt; image refs
# from images.txt. Env: REGISTRY, IMAGE_NAMESPACE, COSIGN_KEY, COSIGN_PASSWORD.
set -euo pipefail
: "${REGISTRY:?}" "${IMAGE_NAMESPACE:?}" "${COSIGN_KEY:?}"

for chart in kora-crds kora; do
  digest=$(grep -oE 'sha256:[0-9a-f]{64}' "push-${chart}.txt" | head -1)
  [ -n "${digest}" ] || { echo "::error::no chart digest in push-${chart}.txt" >&2; exit 1; }
  cosign sign --yes --key env://COSIGN_KEY "${REGISTRY}/${IMAGE_NAMESPACE}/charts/${chart}@${digest}"
done

# images.txt also lists the third-party images the chart pins, such as OPA. The
# release can sign only the images it publishes under its own namespace.
grep "^${REGISTRY}/${IMAGE_NAMESPACE}/" images.txt | while read -r ref; do
  cosign sign --yes --key env://COSIGN_KEY "${ref}"
done
