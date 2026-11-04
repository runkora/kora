#!/usr/bin/env bash
# Build the kora chart's dependencies from Chart.lock. Istio's charts come from
# an OCI registry, which needs no helm repo add.
set -euo pipefail
helm dependency build charts/kora
