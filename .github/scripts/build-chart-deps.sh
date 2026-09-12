#!/usr/bin/env bash
# Register the chart's dependency repositories, then build them from Chart.lock.
# helm dependency build needs the repos added even though the lock pins versions.
set -euo pipefail
helm repo add istio https://istio-release.storage.googleapis.com/charts >/dev/null
helm dependency build charts/orbit
