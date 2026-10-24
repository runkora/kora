#!/usr/bin/env bash
# Signs every file in the release asset directory given as $1 with keyless
# cosign, and writes each Sigstore bundle next to it as <file>.sigstore.json:
# the name `cosign verify-blob --bundle` takes and OpenSSF Scorecard looks for.
set -euo pipefail
cd "${1:?asset directory}"

for f in *; do
  cosign sign-blob --yes --bundle "${f}.sigstore.json" "${f}" > /dev/null
done
