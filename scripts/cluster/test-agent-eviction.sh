#!/usr/bin/env bash
# Checks that a bound agent pod is protected from eviction only while a run holds it on the local cluster
# (docs/adr/2026-11-22-agent-cold-tier.md, O3 and Amendment 1).
#
# It creates an agent at 1 bound pod, starts a run and leaves it idle, then evicts the pod through the
# Eviction API. It asserts:
#   1. the tenant has the agent-busy and sandbox-busy PodDisruptionBudgets, the pod carries runkora.io/busy,
#      and the eviction is refused;
#   2. after the run ends, the scaler removes the label and the eviction succeeds;
#   3. the scaler binds a replacement pool pod, and the agent's next run runs on it with the label.
# The agent and its runs are deleted at the end.
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
pod_busy() { [[ $(k get pod "$1" -o jsonpath='{.metadata.labels.runkora\.io/busy}' 2>/dev/null) == true ]]; }
pod_not_busy() { ! pod_busy "$1"; }
# live_pods prints the agent's bound pods that are not terminating.
live_pods() {
  k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.deletionTimestamp}{"\n"}{end}' | awk 'NF == 1 {print $1}'
}
one_live_ready_pod() { [[ $(live_pods | wc -l | tr -d ' ') == 1 ]] && pod_ready "$(live_pods)"; }
evict() {
  k create --raw "/api/v1/namespaces/$NS/pods/$1/eviction" -f - >/dev/null <<JSON
{"apiVersion": "policy/v1", "kind": "Eviction", "metadata": {"name": "$1", "namespace": "$NS"}}
JSON
}
start_run() {
  api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id
}

agent="" slug="" run=""
cleanup() {
  [[ -n $run ]] && api DELETE "/api/v1/runs/$run" >/dev/null 2>&1 || true
  [[ -n $agent ]] && api DELETE "/api/v1/agent-types/$agent" >/dev/null 2>&1 || true
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
for pdb in agent-busy sandbox-busy; do
  [[ $(k get poddisruptionbudget "$pdb" -o jsonpath='{.spec.maxUnavailable}') == 0 ]] || fail "$NS has no $pdb budget with maxUnavailable 0"
done

run=$(start_run)
wait_for "the run to finish its turn" run_idle "$run"
[[ $(run_field "$run" pod_name) == "$old_pod" ]] || fail "the run is not on $old_pod"
pod_busy "$old_pod" || fail "$old_pod holds run $run and is not labelled busy"
echo "agent $slug is up with 1 pod, and run $run is idle on $old_pod"

echo "== Evict a busy pod"
refusal=$(evict "$old_pod" 2>&1) && fail "the eviction of $old_pod was allowed while run $run holds it"
[[ $refusal == *TooManyRequests* ]] || fail "the eviction of $old_pod failed for another reason: $refusal"
pod_terminating "$old_pod" && fail "$old_pod is terminating after a refused eviction"
echo "PASS 1: the eviction of $old_pod is refused while run $run holds it"

echo "== Evict an idle pod"
api DELETE "/api/v1/runs/$run" >/dev/null
run=""
wait_for "the scaler to remove the busy label from $old_pod" pod_not_busy "$old_pod"
evict "$old_pod" || fail "the eviction of the idle $old_pod was refused"
wait_for "$old_pod to start terminating" pod_terminating "$old_pod"
echo "PASS 2: with no run on it, $old_pod lost the busy label and was evicted"

echo "== Next run"
wait_for "a replacement pod to be bound and Ready" one_live_ready_pod
new_pod=$(live_pods)
[[ $new_pod != "$old_pod" ]] || fail "the agent's pod is still $old_pod"
run=$(start_run)
wait_for "the next run to finish its turn" run_idle "$run"
[[ $(run_field "$run" pod_name) == "$new_pod" ]] || fail "the next run is not on $new_pod"
pod_busy "$new_pod" || fail "$new_pod holds run $run and is not labelled busy"
echo "PASS 3: the next run runs on the replacement $new_pod, which is labelled busy"
