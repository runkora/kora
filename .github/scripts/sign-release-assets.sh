#!/usr/bin/env bash
# Signs every file in the release asset directory given as $1 with keyless
# cosign, and writes each Sigstore bundle next to it as <file>.sigstore.json:
# the name `cosign verify-blob --bundle` takes and OpenSSF Scorecard looks for.
# Prints the assets' sha256sum lines, base64-encoded, as the subjects of the
# SLSA provenance.
set -euo pipefail
cd "${1:?asset directory}"

assets=(*)
for f in "${assets[@]}"; do
  cosign sign-blob --yes --bundle "${f}.sigstore.json" "${f}" > /dev/null
done
sha256sum "${assets[@]}" | base64 -w0
