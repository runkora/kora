#!/usr/bin/env bash
# Checks the surge rollout of an agent's session-manager pods on the local cluster
# (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 1) with a real Claude Code harness.
#
# It creates an agent at the floor of 1 pod and sets an extra environment variable on its
# StatefulSet. The kora-agents apply at startup removes it, so a restarted kora-agents pod changes
# the pod template through a real apply. With a run idle on the old pod, it asserts:
#   1. the apply raises the replicas by one and sets the partition to the old replicas in one write;
#   2. the surge pod is Ready before the old pod starts terminating;
#   3. a run started while the old pod drains lands on the surge pod and completes its turn;
#   4. the run on the old pod completes a turn while the pod drains, and ends terminated, not failed;
#   5. after the runs end, the agent is back to 1 pod on the new revision.
# The agent and its runs are deleted at the end.
#
# Usage: scripts/cluster/test-agent-surge-rollout.sh (or make test-agent-surge-rollout)
# Needs make auth (/tmp/kora_credentials.json). KORA_KUBE_CONTEXT defaults to k3d-kora-local and
# MODEL to anthropic/claude-haiku-4-5. It restarts the kora-agents pods.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
SYSTEM_NS=${SYSTEM_NAMESPACE:-kora-system}
CONTEXT=${KORA_KUBE_CONTEXT:-k3d-kora-local}
MODEL=${MODEL:-anthropic/claude-haiku-4-5}
TIMEOUT_SECS=${TIMEOUT_SECS:-600}
PROMPT='Reply with the single word ok. Do not use any tools.'

k() { kubectl --context "$CONTEXT" -n "$NS" "$@"; }

api() {
  local method=$1 path=$2
  shift 2
  curl -sfk -X "$method" -H "Authorization: Bearer $(jq -r .access_token /tmp/kora_credentials.json)" \
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
# last_seq prints the sequence number of the run's last event.
last_seq() { api GET "/api/v1/runs/$1/events?after_seq=${2:-0}&limit=200" -H 'Accept: application/json' | jq -r .next_seq; }

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

agent="" slug="" pods_pid="" sts_pid="" pods_log="" sts_log=""
runs=()
cleanup() {
  [[ -n $pods_pid ]] && kill "$pods_pid" 2>/dev/null || true
  [[ -n $sts_pid ]] && kill "$sts_pid" 2>/dev/null || true
  for run in ${runs[@]+"${runs[@]}"}; do api DELETE "/api/v1/runs/$run" >/dev/null 2>&1 || true; done
  [[ -n $agent ]] && api DELETE "/api/v1/agent-types/$agent" >/dev/null 2>&1 || true
  rm -f "$pods_log" "$sts_log"
}
trap cleanup EXIT

start_run() {
  api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id
}

echo "== Setup"
slug="surge-check-$(date +%s)"
provider=${MODEL%%/*} name=${MODEL#*/}
agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg model "$MODEL" \
  --arg scope "inference:${provider}:${name}:use" \
  '{name: $slug, slug: $slug, framework: "claude-code", model_id: $model, system_prompt: "Answer in one word.",
    identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"}}')" | jq -r .id)
k wait --for=create "statefulset/$slug" --timeout=180s >/dev/null
k rollout status "statefulset/$slug" --timeout=300s >/dev/null
[[ $(k get statefulset "$slug" -o jsonpath='{.spec.replicas}') == 1 ]] || fail "agent $slug did not start at 1 pod"
# The apply renders no such variable, so the next apply changes the pod template back.
k set env "statefulset/$slug" -c session-manager KORA_SURGE_CHECK=1 >/dev/null
k rollout status "statefulset/$slug" --timeout=300s >/dev/null
old_uid=$(pod_uid "$slug-0")
old_revision=$(k get statefulset "$slug" -o jsonpath='{.status.updateRevision}')
echo "agent $slug is up with 1 pod ($slug-0, revision $old_revision)"

old_run=$(start_run)
runs+=("$old_run")
wait_for "the first run to finish its turn" run_idle "$old_run"
[[ $(run_field "$old_run" pod_ordinal) == 0 ]] || fail "the first run is not on $slug-0"
echo "run $old_run is idle on $slug-0"

# Each watch line is one write: the StatefulSet's spec, and each pod's readiness and deletion.
sts_log=$(mktemp) pods_log=$(mktemp)
kubectl --context "$CONTEXT" -n "$NS" get statefulset "$slug" --watch -o jsonpath='{.metadata.generation} {.spec.replicas} {.spec.updateStrategy.rollingUpdate.partition}{"\n"}' >"$sts_log" 2>/dev/null &
sts_pid=$!
kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" --watch \
  -o jsonpath='{.metadata.name} {.metadata.uid} {.status.conditions[?(@.type=="Ready")].status} {.metadata.deletionTimestamp}{"\n"}' >"$pods_log" 2>/dev/null &
pods_pid=$!
wait_for "the watches to start" test -s "$sts_log" -a -s "$pods_log"

echo "== Roll"
kubectl --context "$CONTEXT" -n "$SYSTEM_NS" delete pod -l app=kora-agents --wait=false >/dev/null
echo "restarted kora-agents; its apply at startup changes the pod template of $slug"
wait_for "the apply to change the pod template" \
  test "$(k get statefulset "$slug" -o jsonpath='{.status.updateRevision}')" != "$old_revision"
# The first event is the state before the roll. Each later event is one write.
read -r gen from to pfrom pto < <(awk 'NR == 1 {g = $1; r = $2; p = $3 + 0} NR > 1 && $2 != r {print $1 - g, r, $2, p, $3 + 0; exit}' "$sts_log") ||
  fail "no write raised the replicas"
(( gen == 1 && from == 1 && to == 2 && pfrom == 0 && pto == 1 )) ||
  fail "the replicas went $from to $to and the partition $pfrom to $pto in $gen writes, want 1 to 2 and 0 to 1 in one"
echo "PASS 1: one write raised the replicas from 1 to 2 and set the partition to 1"

wait_for "$slug-0 to start terminating" pod_terminating "$slug-0"
ready_line=$(awk -v p="$slug-1" '$1 == p && $3 == "True" {print NR; exit}' "$pods_log")
term_line=$(awk -v p="$slug-0" -v u="$old_uid" '$1 == p && $2 == u && NF >= 4 {print NR; exit}' "$pods_log")
[[ -n $ready_line && -n $term_line ]] || fail "the pod watch has no Ready $slug-1 or terminating $slug-0"
(( ready_line < term_line )) || fail "$slug-0 started terminating before $slug-1 was Ready"
echo "PASS 2: $slug-1 was Ready before $slug-0 started terminating"

new_run=$(start_run)
runs+=("$new_run")
wait_for "the run started during the roll to finish its turn" run_idle "$new_run"
[[ $(run_field "$new_run" pod_ordinal) == 1 ]] || fail "the run started during the roll is not on $slug-1"
pod_terminating "$slug-0" && [[ $(pod_uid "$slug-0") == "$old_uid" ]] || fail "$slug-0 stopped draining before the check"
echo "PASS 3: run $new_run started while $slug-0 drained, landed on $slug-1 and finished its turn"

seq=$(last_seq "$old_run")
api POST "/api/v1/runs/$old_run/messages" -d "$(jq -n --arg m "$PROMPT" '{content: $m}')" >/dev/null
# The turn is done once the run has new events and is idle again.
answered() { (( $(last_seq "$old_run" "$seq") > seq )) && run_idle "$old_run"; }
wait_for "the run on the draining pod to finish a turn" answered
pod_terminating "$slug-0" && [[ $(pod_uid "$slug-0") == "$old_uid" ]] || fail "$slug-0 stopped draining before the turn"
api DELETE "/api/v1/runs/$old_run" >/dev/null
wait_for "the old pod's run to end" test "$(run_field "$old_run" state)" != idle
state=$(run_field "$old_run" state)
[[ $state == terminated ]] || fail "the old pod's run ended $state, want terminated"
echo "PASS 4: run $old_run finished a turn on the draining $slug-0 and ended $state"

echo "== Back to the floor"
rolled() {
  local current update
  read -r current update < <(k get statefulset "$slug" -o jsonpath='{.status.currentRevision} {.status.updateRevision}{"\n"}')
  [[ $current == "$update" ]]
}
wait_for "the roll to finish" rolled
api DELETE "/api/v1/runs/$new_run" >/dev/null
at_floor() {
  [[ $(k get statefulset "$slug" -o jsonpath='{.spec.replicas}') == 1 && $(k get pods -l "app=$slug" -o name | wc -l | tr -d ' ') == 1 ]]
}
wait_for "the agent to return to 1 pod" at_floor
if [[ $(pod_uid "$slug-0") == "$old_uid" ]] || ! pod_ready "$slug-0"; then fail "$slug-0 was not replaced on the new revision"; fi
echo "PASS 5: the agent is back to 1 pod, $slug-0 on revision $(k get statefulset "$slug" -o jsonpath='{.status.updateRevision}')"

echo "StatefulSet writes (generation, replicas, partition):"
sed 's/^/  /' "$sts_log"
echo "pod events (name, uid, Ready, deletion):"
sed 's/^/  /' "$pods_log"
