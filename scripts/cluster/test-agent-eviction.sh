#!/usr/bin/env bash
# Checks that an evicted only pod of an agent at the floor of 1 gets a replacement on the local cluster
# (docs/adr/2026-11-22-agent-cold-tier.md, O3 and D1b).
#
# It creates an agent at 1 bound pod, starts a run and leaves it idle, then evicts the pod through the
# Eviction API. No PodDisruptionBudget covers the pod, and the idle run holds it in its drain. It asserts:
#   1. the scaler binds a replacement pool pod that is Ready while the evicted pod still drains;
#   2. after the run ends and the evicted pod exits, the agent is back to 1 pod;
#   3. from the replacement's first Ready until then, the agent always has a Ready pod that is not
#      terminating.
# The agent and its run are deleted at the end.
#
# Usage: scripts/cluster/test-agent-eviction.sh (or make test-agent-eviction)
# Needs make auth (/tmp/kora_credentials.json). KORA_KUBE_CONTEXT defaults to k3d-kora-local and MODEL to
# anthropic/claude-haiku-4-5.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
CONTEXT=${KORA_KUBE_CONTEXT:-k3d-kora-local}
MODEL=${MODEL:-anthropic/claude-haiku-4-5}
TIMEOUT_SECS=${TIMEOUT_SECS:-600}
# One hung API or kubectl call fails that call. wait_for then retries it until TIMEOUT_SECS.
CALL_TIMEOUT_SECS=${CALL_TIMEOUT_SECS:-30}
PROMPT='Reply with the single word ok. Do not use any tools.'

k() { kubectl --context "$CONTEXT" -n "$NS" --request-timeout="${CALL_TIMEOUT_SECS}s" "$@"; }

api() {
  local method=$1 path=$2
  shift 2
  curl -sSfk --max-time "$CALL_TIMEOUT_SECS" -X "$method" -H "Authorization: Bearer $(jq -r .access_token /tmp/kora_credentials.json)" \
    -H 'Content-Type: application/json' "$API$path" "$@"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# wait_for runs the condition command until it succeeds, failing after TIMEOUT_SECS.
wait_for() {
  local what=$1 deadline=$((SECONDS + TIMEOUT_SECS))
  shift
  until "$@"; do
    (( SECONDS < deadline )) || fail "timed out after ${TIMEOUT_SECS}s waiting for $what"
    sleep 3
  done
}

run_field() { api GET "/api/v1/runs/$1" | jq -r ".$2"; }

# run_idle succeeds once the run's turn is done. A failed run fails the check.
run_idle() {
  local state
  state=$(run_field "$1" state)
  [[ $state == failed ]] && fail "run $1 failed: $(api GET "/api/v1/runs/$1")"
  [[ $state == idle ]]
}

pod_ready() { [[ $(k get pod "$1" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null) == True ]]; }
pod_terminating() { [[ -n $(k get pod "$1" -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null) ]]; }
# live_pods prints the agent's bound pods that are not terminating.
live_pods() {
  k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.deletionTimestamp}{"\n"}{end}' | awk 'NF == 1'
}
one_live_ready_pod() { [[ $(live_pods | wc -l | tr -d ' ') == 1 ]] && pod_ready "$(live_pods)"; }
# old_pod_draining succeeds while the evicted pod is still terminating.
old_pod_draining() { pod_terminating "$old_pod"; }

agent="" slug="" run="" pods_pid="" pods_log=""
cleanup() {
  [[ -n $pods_pid ]] && kill "$pods_pid" 2>/dev/null || true
  [[ -n $run ]] && api DELETE "/api/v1/runs/$run" >/dev/null 2>&1 || true
  [[ -n $agent ]] && api DELETE "/api/v1/agent-types/$agent" >/dev/null 2>&1 || true
  rm -f "$pods_log"
}
trap cleanup EXIT

echo "== Setup"
slug="eviction-check-$(date +%s)"
provider=${MODEL%%/*} name=${MODEL#*/}
agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg model "$MODEL" \
  --arg scope "inference:${provider}:${name}:use" \
  '{name: $slug, slug: $slug, framework: "claude-code", model_id: $model, system_prompt: "Answer in one word.",
    identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"}}')" | jq -r .id)
wait_for "agent $slug to have 1 Ready bound pod" one_live_ready_pod
old_pod=$(live_pods)
k get poddisruptionbudget "$slug" >/dev/null 2>&1 && fail "agent $slug has a PodDisruptionBudget"

run=$(api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id)
wait_for "the run to finish its turn" run_idle "$run"
[[ $(run_field "$run" pod_name) == "$old_pod" ]] || fail "the run is not on $old_pod"
echo "agent $slug is up with 1 pod, and run $run is idle on $old_pod"

# Each watch line is one pod event: its type, name, readiness and deletion.
pods_log=$(mktemp)
kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" --watch --output-watch-events \
  -o jsonpath='{.type} {.object.metadata.name} {.object.status.conditions[?(@.type=="Ready")].status} {.object.metadata.deletionTimestamp}{"\n"}' >"$pods_log" 2>/dev/null &
pods_pid=$!
wait_for "the pod watch to start" test -s "$pods_log"

echo "== Evict"
k create --raw "/api/v1/namespaces/$NS/pods/$old_pod/eviction" -f - >/dev/null <<JSON
{"apiVersion": "policy/v1", "kind": "Eviction", "metadata": {"name": "$old_pod", "namespace": "$NS"}}
JSON
wait_for "$old_pod to start terminating" pod_terminating "$old_pod"
echo "evicted $old_pod, which drains while run $run is idle on it"
wait_for "a replacement pod to be bound and Ready" one_live_ready_pod
new_pod=$(live_pods)
old_pod_draining || fail "$old_pod exited before $new_pod was Ready"
echo "PASS 1: $new_pod is Ready while the evicted $old_pod still drains"

echo "== Back to the floor"
api DELETE "/api/v1/runs/$run" >/dev/null
old_pod_gone() { ! k get pod "$old_pod" >/dev/null 2>&1; }
wait_for "the evicted pod to exit" old_pod_gone
wait_for "the agent to return to 1 pod" one_live_ready_pod
[[ $(live_pods) == "$new_pod" ]] || fail "the agent's pod is $(live_pods), want $new_pod"
echo "PASS 2: the evicted pod exited and the agent is back to 1 pod"

kill -0 "$pods_pid" 2>/dev/null || fail "the pod watch exited early"
echo "pod events (type, name, Ready, deletion):"
sed 's/^/  /' "$pods_log"

# Replays the pod events from the replacement's first Ready. It prints the first event that leaves
# no Ready pod that is not terminating.
first_gap=$(awk -v p="$new_pod" '
  { live[$2] = $1 != "DELETED" && $3 == "True" && $4 == "" }
  $2 == p && live[$2] { replaced = 1 }
  replaced {
    n = 0
    for (name in live) n += live[name]
    if (n == 0) { print NR ": " $0; exit }
  }' "$pods_log")
[[ -z $first_gap ]] || fail "the agent had no Ready pod that is not terminating at pod event $first_gap"
echo "PASS 3: from the replacement's first Ready until it was back to 1 pod, the agent always had a Ready pod that is not terminating"
