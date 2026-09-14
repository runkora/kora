#!/usr/bin/env bash
# Prints a short content hash of every product migration file. `make redeploy`
# compares it with the hash recorded at the last migrate image push to decide
# whether the schema hook needs to run again. cmd/example is not migrated.
set -euo pipefail
cd "$(dirname "$0")/../.."
find backend/cmd -path '*/migrations/*.sql' -not -path '*/example/*' | sort | xargs shasum | shasum | cut -c1-16
