#!/usr/bin/env bash
# Checks demand-driven agent scaling on bound pool pods on the local cluster
# (docs/adr/2026-11-09-demand-driven-agent-scaling.md, docs/adr/2026-11-22-agent-cold-tier.md)
# with a real Claude Code harness.
#
# It creates an agent with max_concurrent_runs TOTAL, checks its first pod is a bound pool pod,
# starts TOTAL + 1 one-turn runs, then asserts:
#   1. one scaler pass binds more than one pool pod to the agent;
#   2. the run over the total stays pending past the old start budget (3 minutes);
#   3. after the runs are terminated, the agent's pods fall to the floor one pod per scaler pass.
# The agent and its runs are deleted at the end.
#
# Usage: scripts/cluster/test-agent-scaling.sh (or make test-agent-scaling)
# Needs make auth (/tmp/kora_credentials.json). KORA_KUBE_CONTEXT defaults to k3d-kora-local,
# MODEL to anthropic/claude-haiku-4-5, and FLOOR to the chart's agents minReplicas of 1.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
CONTEXT=${KORA_KUBE_CONTEXT:-k3d-kora-local}
MODEL=${MODEL:-anthropic/claude-haiku-4-5}
TOTAL=${TOTAL:-30}
FLOOR=${FLOOR:-1}
OLD_BUDGET_SECS=180
# The scaler checks each agent every 5 seconds.
SCALER_PASS_SECS=5
BIND_TIMEOUT_SECS=${BIND_TIMEOUT_SECS:-180}
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

# live_pods prints the agent's bound pods that are not terminating.
live_pods() {
  k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.deletionTimestamp}{"\n"}{end}' | awk 'NF == 1 {print $1}'
}

# ready_pods counts the agent's Ready bound pods.
ready_pods() {
  k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' | grep -c '^True$' || true
}

# first_events prints "time pod" for each pod's first watch event of a kind at or after since: ADDED is the
# bind, which adds the pod to the app selector, and DELETING is the first event with a deletion timestamp.
first_events() {
  awk -v want="$1" -v since="${2:-0}" '
    $1 >= since && !seen[$3] {
      if (want == "ADDED" && $2 == "ADDED") { seen[$3] = 1; print $1, $3 }
      if (want == "DELETING" && NF >= 4) { seen[$3] = 1; print $1, $3 }
    }' "$watch_log"
}

# watch_alive fails the check when the pod watch stopped, which would read as no scaling.
watch_alive() {
  kill -0 "$watch_pid" 2>/dev/null && [[ -s $watch_log ]] || fail "the pod watch stopped"
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

echo "== Setup: max_concurrent_runs $TOTAL, $((TOTAL + 1)) runs, floor $FLOOR"
slug="scale-check-$(date +%s)"
provider=${MODEL%%/*} name=${MODEL#*/}
agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg model "$MODEL" --argjson total "$TOTAL" \
  --arg scope "inference:${provider}:${name}:use" \
  '{name: $slug, slug: $slug, framework: "claude-code", model_id: $model, system_prompt: "Answer in one word.",
    identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"},
    max_concurrent_runs: $total}')" | jq -r .id)
deadline=$((SECONDS + BIND_TIMEOUT_SECS))
until (( $(ready_pods) >= FLOOR )); do
  (( SECONDS < deadline )) || fail "agent $slug has no Ready bound pod after ${BIND_TIMEOUT_SECS}s"
  sleep 2
done
first=$(live_pods | head -1)
# The fields are split on | so an empty pool label keeps its place.
IFS='|' read -r agent_label pool_label owners < <(k get pod "$first" -o jsonpath='{.metadata.labels.spiffe\.io/agent}|{.metadata.labels.runkora\.io/pool}|{.metadata.ownerReferences[*].kind}{"\n"}')
[[ $agent_label == "$slug" && -z $pool_label && $owners != *ReplicaSet* ]] ||
  fail "$first carries spiffe.io/agent=$agent_label, runkora.io/pool=$pool_label and owners $owners; want a pod bound to $slug"
k get statefulset "$slug" >/dev/null 2>&1 && fail "agent $slug has a StatefulSet"
echo "agent $slug is up with $(live_pods | wc -l | tr -d ' ') bound pod(s), first $first"

# Each watch line is "time type pod [deletion]".
watch_log=$(mktemp)
# Run kubectl directly, not through k, so $! is kubectl itself and the cleanup kill stops the watch.
kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" --watch --output-watch-events \
  -o jsonpath='{.type} {.object.metadata.name} {.object.metadata.deletionTimestamp}{"\n"}' \
  > >(while read -r line; do echo "$(date +%s) $line"; done > "$watch_log") 2>/dev/null &
watch_pid=$!
# The watch's first events list the current pods; a bind before them would be missed.
until [[ -s $watch_log ]]; do
  kill -0 "$watch_pid" 2>/dev/null || fail "the pod watch did not start"
  sleep 1
done

start=$SECONDS
runs_from=$(date +%s)
for _ in $(seq 0 "$TOTAL"); do
  runs+=("$(api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id)")
done
started=$SECONDS
last_run=${runs[${#runs[@]}-1]}
echo "started $((TOTAL + 1)) runs in $((started - start))s"

# status prints elapsed seconds, the live and Ready pods and the run states, and sets on_pod, pending,
# ended and pending_ids.
status() {
  local states
  states=$(api GET "/api/v1/runs?agent_type_id=$agent&limit=200" | jq -r '.runs[] | "\(.state) \(.id)"')
  on_pod=$(grep -cE '^(starting|active|idle) ' <<<"$states" || true)
  pending=$(grep -c '^pending ' <<<"$states" || true)
  ended=$(grep -cE '^(completing|completed|failed|budget_exhausted|terminated) ' <<<"$states" || true)
  pending_ids=$(awk '$1 == "pending" {print $2}' <<<"$states")
  printf 't=%4ss pods=%s ready=%s | runs %s\n' "$((SECONDS - start))" \
    "$(live_pods | wc -l | tr -d ' ')" "$(ready_pods)" \
    "$(awk '{print $1}' <<<"$states" | sort | uniq -c | awk '{printf "%s=%s ", $2, $1}')"
}

echo "== Scale-up"
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
echo "binds after the runs started (time, pod):"
first_events ADDED "$runs_from" | indent
first_events ADDED "$runs_from" | awk -v pass="$SCALER_PASS_SECS" '{t[NR] = $1} END {for (i = 2; i <= NR; i++) if (t[i] - t[i-1] < pass) exit 0; exit 1}' ||
  fail "no scaler pass bound more than one pod"
echo "PASS 1: one scaler pass bound more than one pool pod"

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
  (( $(live_pods | wc -l | tr -d ' ') == FLOOR )) && break
  (( SECONDS - end_start < SCALE_DOWN_TIMEOUT_SECS )) || fail "$(live_pods | wc -l | tr -d ' ') pods after ${SCALE_DOWN_TIMEOUT_SECS}s, want $FLOOR"
  sleep 5
done
watch_alive
echo "pods removed after the runs ended (time, pod):"
first_events DELETING "$ended_at" | indent
# A pass removes one pod, so two removals land most of a pass apart.
first_events DELETING "$ended_at" | awk -v pass="$SCALER_PASS_SECS" '{t[NR] = $1} END {if (NR == 0) exit 1; for (i = 2; i <= NR; i++) if (t[i] - t[i-1] < pass - 2) exit 1}' ||
  fail "a scaler pass removed more than one pod"
echo "PASS 3: the agent is back to $FLOOR pods $((SECONDS - end_start))s after the runs ended, one pod per pass"

echo "scaler log for the agent:"
kubectl --context "$CONTEXT" -n kora-system logs -l app=kora-agents -c kora-agents --since=1h --tail=-1 2>/dev/null |
  grep "$agent" | grep -E 'bound agent pod|removed empty agent pod' | sed $'s/\x1b\\[[0-9;]*m//g; s/^/  /' || true
