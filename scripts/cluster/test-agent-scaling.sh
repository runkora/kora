#!/usr/bin/env bash
# Checks demand-driven agent scaling on the local cluster
# (docs/adr/2026-11-09-demand-driven-agent-scaling.md) with a real Claude Code harness.
#
# Parallel switch: for each agent StatefulSet still on OrderedReady, it records the pods,
# waits for the kora-agents scaler to recreate the StatefulSet as Parallel, which happens
# once the agent has no active or starting run, and asserts its pods that were not
# terminating remain with the same names and UIDs. It skips this part when every agent
# StatefulSet is already on Parallel.
#
# Scaling: it creates an agent with max_concurrent_runs TOTAL and starts TOTAL + 1 one-turn
# runs, then asserts:
#   1. one write to the StatefulSet raises its replicas by more than one;
#   2. the run over the total stays pending past the old start budget (3 minutes);
#   3. after the runs are terminated, the replicas fall to the floor one pod per write and
#      the agent is left with that many pods.
# The agent and its runs are deleted at the end.
#
# Usage: scripts/cluster/test-agent-scaling.sh (or make test-agent-scaling)
# Needs make auth (/tmp/kora_credentials.json). KUBE_CONTEXT defaults to k3d-kora-local,
# MODEL to anthropic/claude-haiku-4-5, and FLOOR to the chart's agents minReplicas of 2.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
CONTEXT=${KUBE_CONTEXT:-k3d-kora-local}
MODEL=${MODEL:-anthropic/claude-haiku-4-5}
TOTAL=${TOTAL:-30}
FLOOR=${FLOOR:-2}
OLD_BUDGET_SECS=180
# An idle run blocks the switch, and a person's idle run lasts 1200 s by default.
SWITCH_TIMEOUT_SECS=${SWITCH_TIMEOUT_SECS:-1500}
SCALE_UP_TIMEOUT_SECS=${SCALE_UP_TIMEOUT_SECS:-900}
SCALE_DOWN_TIMEOUT_SECS=${SCALE_DOWN_TIMEOUT_SECS:-600}
PROMPT='Reply with the single word ok. Do not use any tools.'

k() { kubectl --context "$CONTEXT" -n "$NS" "$@"; }

api() {
  local method=$1 path=$2
  shift 2
  curl -sfk -X "$method" -H "Authorization: Bearer $(jq -r .access_token /tmp/kora_credentials.json)" \
    -H 'Content-Type: application/json' "$API$path" "$@"
}

indent() { sed 's/^/  /'; }

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# pod_ids prints "name uid" for each pod of a StatefulSet that is not terminating, sorted by name.
pod_ids() {
  k get pods -l "app=$1" -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.uid} {.metadata.deletionTimestamp}{"\n"}{end}' |
    awk 'NF == 2 {print $1, $2}' | sort
}

# replica_steps prints "time from to generation_delta" for each replica change in a watch log
# at or after the time given. Earlier lines still set the starting replicas.
replica_steps() {
  awk -v since="${2:-0}" '
    NF >= 3 {
      if (seen && $3 != replicas && $1 >= since) print $1, replicas, $3, $2 - generation
      seen = 1; replicas = $3; generation = $2
    }' "$1"
}

# watch_alive fails the check when the StatefulSet watch stopped, which would read as no scaling.
watch_alive() {
  kill -0 "$watch_pid" 2>/dev/null && [[ -s $watch_log ]] || fail "the StatefulSet watch stopped"
}

agent="" slug="" watch_pid="" watch_log=""
runs=()
cleanup() {
  [[ -n $watch_pid ]] && kill "$watch_pid" 2>/dev/null || true
  for run in ${runs[@]+"${runs[@]}"}; do api DELETE "/api/v1/runs/$run" >/dev/null 2>&1 || true; done
  [[ -n $agent ]] && api DELETE "/api/v1/agent-types/$agent" >/dev/null 2>&1 || true
  [[ -n $watch_log ]] && rm -f "$watch_log"
}
trap cleanup EXIT

echo "== Parallel switch"
switched=0
statefulsets=$(k get statefulsets -l runkora.io/agent-type -o jsonpath='{range .items[*]}{.metadata.name} {.spec.podManagementPolicy}{"\n"}{end}')
while read -r sts policy; do
  [[ -z $sts || $policy == Parallel ]] && continue
  before=$(pod_ids "$sts")
  uid=$(k get statefulset "$sts" -o jsonpath='{.metadata.uid}')
  echo "$sts is on $policy (StatefulSet $uid) with pods:"
  indent <<<"$before"
  echo "waiting for the scaler to switch it; an agent with an active, idle or starting run switches once its runs end"
  deadline=$((SECONDS + SWITCH_TIMEOUT_SECS))
  until [[ $(k get statefulset "$sts" -o jsonpath='{.spec.podManagementPolicy}' 2>/dev/null) == Parallel ]]; do
    (( SECONDS < deadline )) || fail "$sts still not on Parallel after ${SWITCH_TIMEOUT_SECS}s"
    sleep 2
  done
  # Wait for the StatefulSet controller to adopt the orphaned pods, or to replace them if adoption fails.
  sleep 15
  after=$(pod_ids "$sts")
  echo "$sts is on Parallel (StatefulSet $(k get statefulset "$sts" -o jsonpath='{.metadata.uid}')) with pods:"
  indent <<<"$after"
  # A pod replaced under the same name has a new UID. A pod that was terminating, such as one an
  # apply is rolling, is not in before, and one the scaler removed is in neither.
  replaced=$(join <(echo "$before") <(echo "$after") | awk '$2 != $3 {print $1}')
  [[ -z $replaced ]] || fail "the switch replaced pods of $sts: $replaced"
  echo "PASS: $sts switched to Parallel and kept its pods"
  switched=$((switched + 1))
done <<<"$statefulsets"
(( switched > 0 )) || echo "no agent StatefulSet is on OrderedReady; skipped"

echo "== Scaling: max_concurrent_runs $TOTAL, $((TOTAL + 1)) runs, floor $FLOOR"
slug="scale-check-$(date +%s)"
provider=${MODEL%%/*} name=${MODEL#*/}
agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg model "$MODEL" --argjson total "$TOTAL" \
  --arg scope "inference:${provider}:${name}:use" \
  '{name: $slug, slug: $slug, framework: "claude-code", model_id: $model, system_prompt: "Answer in one word.",
    identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"},
    max_concurrent_runs: $total}')" | jq -r .id)
k wait --for=create "statefulset/$slug" --timeout=180s >/dev/null
k rollout status "statefulset/$slug" --timeout=300s >/dev/null
[[ $(k get statefulset "$slug" -o jsonpath='{.spec.podManagementPolicy}') == Parallel ]] ||
  fail "a new agent StatefulSet is not on Parallel"
echo "agent $slug is up with $(k get statefulset "$slug" -o jsonpath='{.spec.replicas}') pods"

# Each watch event is one write to the StatefulSet, and a spec write bumps its generation by one.
watch_log=$(mktemp)
# Run kubectl directly, not through k, so $! is kubectl itself and the cleanup kill stops the watch.
kubectl --context "$CONTEXT" -n "$NS" get statefulset "$slug" --watch -o jsonpath='{.metadata.generation} {.spec.replicas} {.status.readyReplicas}{"\n"}' \
  > >(while read -r line; do echo "$(date +%s) $line"; done > "$watch_log") 2>/dev/null &
watch_pid=$!
# The watch's first event is the current state; a scale write before it would be missed.
until [[ -s $watch_log ]]; do sleep 1; done

start=$SECONDS
for _ in $(seq 0 "$TOTAL"); do
  runs+=("$(api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id)")
done
started=$SECONDS
last_run=${runs[${#runs[@]}-1]}
echo "started $((TOTAL + 1)) runs in $((started - start))s"

# status prints elapsed seconds, replicas, Ready and Pending pods and the run states, and sets
# on_pod, pending, ended and pending_ids.
status() {
  local states pods
  states=$(api GET "/api/v1/runs?agent_type_id=$agent&limit=200" | jq -r '.runs[] | "\(.state) \(.id)"')
  pods=$(k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.status.phase}{" "}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}')
  on_pod=$(grep -cE '^(starting|active|idle) ' <<<"$states" || true)
  pending=$(grep -c '^pending ' <<<"$states" || true)
  ended=$(grep -cE '^(completing|completed|failed|budget_exhausted|terminated) ' <<<"$states" || true)
  pending_ids=$(awk '$1 == "pending" {print $2}' <<<"$states")
  printf 't=%4ss replicas=%s ready=%s pending_pods=%s | runs %s\n' "$((SECONDS - start))" \
    "$(k get statefulset "$slug" -o jsonpath='{.spec.replicas}')" "$(grep -c ' True$' <<<"$pods" || true)" \
    "$(grep -c '^Pending' <<<"$pods" || true)" \
    "$(awk '{print $1}' <<<"$states" | sort | uniq -c | awk '{printf "%s=%s ", $2, $1}')"
}

at_total=""
while true; do
  status
  (( ended == 0 )) || fail "$ended runs ended before they were terminated"
  if (( on_pod == TOTAL && pending == 1 )); then
    [[ -n $at_total ]] || at_total=$((SECONDS - start))
    (( SECONDS - started > OLD_BUDGET_SECS + 15 )) && break
  fi
  (( on_pod <= TOTAL )) || fail "$on_pod runs are on pods, over the total of $TOTAL"
  (( SECONDS - start < SCALE_UP_TIMEOUT_SECS )) || fail "$on_pod of $TOTAL runs on pods after ${SCALE_UP_TIMEOUT_SECS}s"
  sleep 10
done
echo "all $TOTAL runs were on pods at t=${at_total}s"

watch_alive
echo "replica changes during scale-up (time, from, to, generation delta):"
replica_steps "$watch_log" | indent
replica_steps "$watch_log" | awk '$3 - $2 > 1 && $4 == 1 {found = 1} END {exit !found}' ||
  fail "no single write raised the replicas by more than one"
echo "PASS 1: one write raised the replicas by more than one"

[[ $pending_ids == "$last_run" ]] && which="run $((TOTAL + 1))" || which="run $pending_ids"
echo "PASS 2: $which is still pending $((SECONDS - started))s after the runs started, past the old ${OLD_BUDGET_SECS}s budget, with $TOTAL runs on pods"

echo "== Scale-down"
ended_at=$(date +%s)
end_start=$SECONDS
# The pending run goes first, so no freed slot starts it.
for run in $pending_ids "${runs[@]}"; do api DELETE "/api/v1/runs/$run" >/dev/null || true; done
echo "terminated the runs in $((SECONDS - end_start))s"
while true; do
  status
  replicas=$(k get statefulset "$slug" -o jsonpath='{.spec.replicas}')
  (( replicas == FLOOR )) && [[ $(k get pods -l "app=$slug" -o name | wc -l | tr -d ' ') == "$FLOOR" ]] && break
  (( SECONDS - end_start < SCALE_DOWN_TIMEOUT_SECS )) || fail "replicas $replicas after ${SCALE_DOWN_TIMEOUT_SECS}s, want $FLOOR"
  sleep 5
done
watch_alive
echo "replica changes after the runs ended (time, from, to, generation delta):"
replica_steps "$watch_log" "$ended_at" | indent
replica_steps "$watch_log" "$ended_at" | awk '{n++} $2 - $3 != 1 || $4 != 1 {bad = 1} END {exit bad || !n}' ||
  fail "a write changed the replicas by other than one pod down"
echo "PASS 3: the agent is back to $FLOOR pods $((SECONDS - end_start))s after the runs ended, one pod per write"

echo "scaler log for the agent:"
kubectl --context "$CONTEXT" -n kora-system logs -l app=kora-agents -c kora-agents --since=1h --tail=-1 2>/dev/null |
  grep "$agent" | grep -E 'scaled agent pods|switching agent' | sed $'s/\x1b\\[[0-9;]*m//g; s/^/  /' || true
