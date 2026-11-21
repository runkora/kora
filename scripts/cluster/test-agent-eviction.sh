#!/usr/bin/env bash
# Checks that an evicted only pod of an agent at the floor of 1 gets a replacement on the local cluster
# (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 1).
#
# It creates an agent at 1 pod, starts a run and leaves it idle, then evicts the pod through the
# Eviction API. The idle run holds the evicted pod in its drain. It asserts:
#   1. a replacement pod at a new ordinal is Ready while the evicted pod still drains;
#   2. after the run ends and the evicted pod exits, the agent is back to 1 pod;
#   3. from the replacement's first Ready until then, the agent always has a Ready pod that is not
#      terminating. The evicted pod turns not Ready during its drain, before the replacement is Ready.
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
# kw runs the kubectl waits, which watch and carry their own --timeout.
kw() { kubectl --context "$CONTEXT" -n "$NS" "$@"; }

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
pod_uid() { k get pod "$1" -o jsonpath='{.metadata.uid}' 2>/dev/null; }
# old_pod_draining succeeds while the evicted pod, by its UID, is still terminating.
old_pod_draining() { pod_terminating "$slug-0" && [[ $(pod_uid "$slug-0") == "$old_uid" ]]; }

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
kw wait --for=create "statefulset/$slug" --timeout=180s >/dev/null
kw rollout status "statefulset/$slug" --timeout=300s >/dev/null
[[ $(k get statefulset "$slug" -o jsonpath='{.spec.replicas}') == 1 ]] || fail "agent $slug did not start at 1 pod"
old_uid=$(pod_uid "$slug-0")

run=$(api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id)
wait_for "the run to finish its turn" run_idle "$run"
[[ $(run_field "$run" pod_ordinal) == 0 ]] || fail "the run is not on $slug-0"
echo "agent $slug is up with 1 pod, and run $run is idle on $slug-0"

# Each watch line is one pod event: its type, name, uid, readiness and deletion.
pods_log=$(mktemp)
kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" --watch --output-watch-events \
  -o jsonpath='{.type} {.object.metadata.name} {.object.metadata.uid} {.object.status.conditions[?(@.type=="Ready")].status} {.object.metadata.deletionTimestamp}{"\n"}' >"$pods_log" 2>/dev/null &
pods_pid=$!
wait_for "the pod watch to start" test -s "$pods_log"

echo "== Evict"
k create --raw "/api/v1/namespaces/$NS/pods/$slug-0/eviction" -f - >/dev/null <<JSON
{"apiVersion": "policy/v1", "kind": "Eviction", "metadata": {"name": "$slug-0", "namespace": "$NS"}}
JSON
wait_for "$slug-0 to start terminating" pod_terminating "$slug-0"
echo "evicted $slug-0, which drains while run $run is idle on it"
wait_for "a replacement pod to be Ready" pod_ready "$slug-1"
old_pod_draining || fail "$slug-0 exited before $slug-1 was Ready"
echo "PASS 1: $slug-1 is Ready while the evicted $slug-0 still drains"

echo "== Back to the floor"
api DELETE "/api/v1/runs/$run" >/dev/null
old_pod_gone() { ! old_pod_draining; }
wait_for "the evicted pod to exit" old_pod_gone
at_floor() {
  [[ $(k get statefulset "$slug" -o jsonpath='{.spec.replicas}') == 1 && $(k get pods -l "app=$slug" -o name | wc -l | tr -d ' ') == 1 ]]
}
wait_for "the agent to return to 1 pod" at_floor
wait_for "$slug-0 to be Ready" pod_ready "$slug-0"
echo "PASS 2: the evicted pod exited and the agent is back to 1 pod"

kill -0 "$pods_pid" 2>/dev/null || fail "the pod watch exited early"
echo "pod events (type, name, uid, Ready, deletion):"
sed 's/^/  /' "$pods_log"

# Replays the pod events from the replacement's first Ready. It prints the first event that leaves
# no Ready pod that is not terminating.
first_gap=$(awk -v p="$slug-1" '
  { live[$3] = $1 != "DELETED" && $4 == "True" && $5 == "" }
  $2 == p && live[$3] { replaced = 1 }
  replaced {
    n = 0
    for (u in live) n += live[u]
    if (n == 0) { print NR ": " $0; exit }
  }' "$pods_log")
[[ -z $first_gap ]] || fail "the agent had no Ready pod that is not terminating at pod event $first_gap"
echo "PASS 3: from the replacement's first Ready until it was back to 1 pod, the agent always had a Ready pod that is not terminating"

