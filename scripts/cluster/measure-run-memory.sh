#!/usr/bin/env bash
# Measures the session-manager working-set growth a run causes, per harness, on
# the local cluster. The largest result sets agents.scheduling.startingRunAllowance
# (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
#
# For each framework it creates a throwaway agent, starts RUNS one-turn runs one
# after another, and reads the working set (memory.current minus inactive_file)
# of the run's pod before the run, every second until its turn ends, and
# SETTLE_SECS after. Runs stay idle with their process alive, so the first run
# measures the first run on a pod and later runs measure each further run.
# The agents and runs are deleted at the end.
#
# Usage: scripts/cluster/measure-run-memory.sh [framework=model ...]
#   default: claude-code=anthropic/claude-haiku-4-5 codex=openai/gpt-5-5 opencode=anthropic/claude-haiku-4-5
# Needs make auth (/tmp/kora_credentials.json). KUBE_CONTEXT defaults to k3d-kora-local.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
CONTEXT=${KUBE_CONTEXT:-k3d-kora-local}
RUNS=${RUNS:-3}
SETTLE_SECS=${SETTLE_SECS:-20}
TURN_TIMEOUT_SECS=${TURN_TIMEOUT_SECS:-240}
PROMPT='Reply with the single word ok. Do not use any tools.'
[[ $# -gt 0 ]] || set -- claude-code=anthropic/claude-haiku-4-5 codex=openai/gpt-5-5 opencode=anthropic/claude-haiku-4-5

api() {
  local method=$1 path=$2
  shift 2
  curl -sfk -X "$method" -H "Authorization: Bearer $(jq -r .access_token /tmp/kora_credentials.json)" \
    -H 'Content-Type: application/json' "$API$path" "$@"
}

# working_set prints a pod's working set in bytes, as the kubelet measures it.
working_set() {
  kubectl --context "$CONTEXT" -n "$NS" exec "$1" -c session-manager -- sh -c \
    'echo $(( $(cat /sys/fs/cgroup/memory.current) - $(awk "/^inactive_file /{print \$2}" /sys/fs/cgroup/memory.stat) ))'
}

mi() { echo $(( $1 / 1048576 )); }

agents=()
runs=()
cleanup() {
  for run in "${runs[@]}"; do api DELETE "/api/v1/runs/$run" >/dev/null || true; done
  for agent in "${agents[@]}"; do api DELETE "/api/v1/agent-types/$agent" >/dev/null || true; done
}
trap cleanup EXIT

echo "framework,model,run,pod,before_mi,peak_mi,settled_mi,peak_growth_mi,settled_growth_mi"
for spec in "$@"; do
  framework=${spec%%=*} model=${spec#*=}
  # A fresh slug each time: a deleted agent's cleanup can still be removing resources under its old name.
  slug="mem-${framework}-$(date +%s)"
  provider=${model%%/*} name=${model#*/}
  agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg fw "$framework" --arg model "$model" \
    --arg scope "inference:${provider}:${name}:use" \
    '{name: $slug, slug: $slug, framework: $fw, model_id: $model, system_prompt: "Answer in one word.",
      identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"}}')" | jq -r .id)
  agents+=("$agent")
  kubectl --context "$CONTEXT" -n "$NS" wait --for=create "statefulset/$slug" --timeout=180s >/dev/null
  kubectl --context "$CONTEXT" -n "$NS" rollout status "statefulset/$slug" --timeout=300s >/dev/null

  for i in $(seq 1 "$RUNS"); do
    declare -A before=()
    for pod in $(kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" -o name 2>/dev/null || true); do
      before[${pod#pod/}]=$(working_set "${pod#pod/}")
    done
    run=$(api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id)
    runs+=("$run")
    peak=0 pod="" state=""
    for _ in $(seq 1 "$TURN_TIMEOUT_SECS"); do
      read -r state ordinal < <(api GET "/api/v1/runs/$run" | jq -r '"\(.state) \(.pod_ordinal)"')
      if [[ -n $ordinal && $ordinal != null && $state != pending && $state != starting ]]; then
        pod="$slug-$ordinal"
        ws=$(working_set "$pod")
        (( ws > peak )) && peak=$ws
      fi
      [[ $state == idle || $state == completed || $state == failed ]] && break
      sleep 1
    done
    if [[ $state != idle ]]; then
      echo "run $run of $framework ended $state; is $model configured?" >&2
      exit 1
    fi
    sleep "$SETTLE_SECS"
    settled=$(working_set "$pod")
    (( settled > peak )) && peak=$settled
    base=${before[$pod]:-$(working_set "$pod")}
    echo "$framework,$model,$i,$pod,$(mi "$base"),$(mi "$peak"),$(mi "$settled"),$(mi $((peak - base))),$(mi $((settled - base)))"
  done
done
