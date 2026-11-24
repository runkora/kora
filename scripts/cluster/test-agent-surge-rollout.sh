#!/usr/bin/env bash
# Checks the roll of an agent's bound pods on the local cluster (docs/adr/2026-11-22-agent-cold-tier.md, O3)
# with a real Claude Code harness.
#
# It creates an agent at the floor of 1 pod and leaves a run idle on it. It then adds a manifest patch
# that annotates the session-manager pod template and restarts kora-agents, whose apply at startup
# rolls the tenant pool Deployment. It asserts:
#   1. a pod on the pool's new template is bound and Ready before the old pod starts terminating;
#   2. a run started while the old pod drains lands on the new pod and completes its turn;
#   3. the run on the old pod completes a turn while the pod drains, and ends terminated, not failed;
#   4. after the runs end, the agent is back to 1 pod on the new template;
#   5. a model change to MODEL2 binds a new pod before the old one starts terminating, and the agent returns to 1 pod.
# The agent, its runs and the patch are removed at the end, and kora-agents restarts again.
#
# Usage: scripts/cluster/test-agent-surge-rollout.sh (or make test-agent-surge-rollout)
# Needs make auth (/tmp/kora_credentials.json). KORA_KUBE_CONTEXT defaults to k3d-kora-local, MODEL to
# anthropic/claude-haiku-4-5 and MODEL2 to anthropic/claude-sonnet-5. It restarts the kora-agents pods.
set -euo pipefail

API=${KORA_API:-https://api.local.runkora.io}
NS=${TENANT_NAMESPACE:-kora-tenant-example}
SYSTEM_NS=${SYSTEM_NAMESPACE:-kora-system}
CONTEXT=${KORA_KUBE_CONTEXT:-k3d-kora-local}
MODEL=${MODEL:-anthropic/claude-haiku-4-5}
MODEL2=${MODEL2:-anthropic/claude-sonnet-5}
TIMEOUT_SECS=${TIMEOUT_SECS:-600}
# One hung API or kubectl call fails that call. wait_for then retries it until TIMEOUT_SECS.
CALL_TIMEOUT_SECS=${CALL_TIMEOUT_SECS:-30}
PATCHES=kora-agents-manifest-patches
PATCH_KEY=zz-roll-check.yaml
PROMPT='Reply with the single word ok. Do not use any tools.'

k() { kubectl --context "$CONTEXT" -n "$NS" --request-timeout="${CALL_TIMEOUT_SECS}s" "$@"; }
ks() { kubectl --context "$CONTEXT" -n "$SYSTEM_NS" --request-timeout="${CALL_TIMEOUT_SECS}s" "$@"; }

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
pod_template() { k get pod "$1" -o jsonpath='{.metadata.annotations.runkora\.io/pool-template}'; }
pool_template() { k get deployment session-manager-pool -o jsonpath='{.spec.template.metadata.annotations.runkora\.io/pool-template}'; }
# live_pods prints the agent's bound pods that are not terminating.
live_pods() {
  k get pods -l "app=$slug" -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.deletionTimestamp}{"\n"}{end}' | awk 'NF == 1 {print $1}'
}
one_live_ready_pod() { [[ $(live_pods | wc -l | tr -d ' ') == 1 ]] && pod_ready "$(live_pods)"; }

restart_agents() {
  ks delete pod -l app=kora-agents --wait=false >/dev/null
  ks rollout status deployment/kora-agents --timeout="${TIMEOUT_SECS}s" >/dev/null
}

agent="" slug="" pods_pid="" pods_log="" patched=""
runs=()
cleanup() {
  [[ -n $pods_pid ]] && kill "$pods_pid" 2>/dev/null || true
  for run in ${runs[@]+"${runs[@]}"}; do api DELETE "/api/v1/runs/$run" >/dev/null 2>&1 || true; done
  [[ -n $agent ]] && api DELETE "/api/v1/agent-types/$agent" >/dev/null 2>&1 || true
  if [[ -n $patched ]]; then
    ks patch configmap "$PATCHES" --type=json -p "[{\"op\": \"remove\", \"path\": \"/data/${PATCH_KEY}\"}]" >/dev/null 2>&1 || true
    restart_agents || true
  fi
  rm -f "$pods_log"
}
trap cleanup EXIT

start_run() {
  api POST /api/v1/runs -d "$(jq -n --arg a "$agent" --arg m "$PROMPT" '{agent_type_id: $a, initial_message: $m}')" | jq -r .id
}

echo "== Setup"
slug="roll-check-$(date +%s)"
provider=${MODEL%%/*} name=${MODEL#*/}
agent=$(api POST /api/v1/agent-types -d "$(jq -n --arg slug "$slug" --arg model "$MODEL" \
  --arg scope "inference:${provider}:${name}:use" \
  '{name: $slug, slug: $slug, framework: "claude-code", model_id: $model, system_prompt: "Answer in one word.",
    identity_mode: "autonomous", grant_scopes: ["inference:invoke", $scope], connectivity: {mode: "none"}}')" | jq -r .id)
wait_for "agent $slug to have 1 Ready bound pod" one_live_ready_pod
old_pod=$(live_pods)
old_template=$(pod_template "$old_pod")
[[ $old_template == "$(pool_template)" ]] || fail "$old_pod is not on the pool's current template"
echo "agent $slug is up with 1 pod ($old_pod, template $old_template)"

old_run=$(start_run)
runs+=("$old_run")
wait_for "the first run to finish its turn" run_idle "$old_run"
[[ $(run_field "$old_run" pod_name) == "$old_pod" ]] || fail "the first run is not on $old_pod"
echo "run $old_run is idle on $old_pod"

# Each watch line is one pod event: its type, name, readiness and deletion.
pods_log=$(mktemp)
kubectl --context "$CONTEXT" -n "$NS" get pods -l "app=$slug" --watch --output-watch-events \
  -o jsonpath='{.type} {.object.metadata.name} {.object.status.conditions[?(@.type=="Ready")].status} {.object.metadata.deletionTimestamp}{"\n"}' >"$pods_log" 2>/dev/null &
pods_pid=$!
wait_for "the pod watch to start" test -s "$pods_log"

echo "== Roll"
patch=$(printf 'target: {kind: StatefulSet}\npatch: |\n  - {op: add, path: /spec/template/metadata/annotations/runkora.io~1roll-check, value: "%s"}\n' "$(date +%s)")
ks patch configmap "$PATCHES" --type=merge -p "$(jq -n --arg k "$PATCH_KEY" --arg v "$patch" '{data: {($k): $v}}')" >/dev/null
patched=1
restart_agents
echo "added a template annotation and restarted kora-agents; its apply at startup rolls the pool"
template_changed() { [[ $(pool_template) != "$old_template" ]]; }
wait_for "the apply to change the pool template" template_changed
new_template=$(pool_template)

wait_for "$old_pod to start terminating" pod_terminating "$old_pod"
new_pod=$(live_pods)
[[ -n $new_pod && $(pod_template "$new_pod") == "$new_template" ]] || fail "no bound pod on the new template $new_template"
ready_line=$(awk -v p="$new_pod" '$2 == p && $3 == "True" {print NR; exit}' "$pods_log")
term_line=$(awk -v p="$old_pod" '$2 == p && NF >= 4 {print NR; exit}' "$pods_log")
[[ -n $ready_line && -n $term_line ]] || fail "the pod watch has no Ready $new_pod or terminating $old_pod"
(( ready_line < term_line )) || fail "$old_pod started terminating before $new_pod was Ready"
echo "PASS 1: $new_pod on the new template was Ready before $old_pod started terminating"

new_run=$(start_run)
runs+=("$new_run")
wait_for "the run started during the roll to finish its turn" run_idle "$new_run"
[[ $(run_field "$new_run" pod_name) == "$new_pod" ]] || fail "the run started during the roll is not on $new_pod"
pod_terminating "$old_pod" || fail "$old_pod stopped draining before the check"
echo "PASS 2: run $new_run started while $old_pod drained, landed on $new_pod and finished its turn"

seq=$(last_seq "$old_run")
api POST "/api/v1/runs/$old_run/messages" -d "$(jq -n --arg m "$PROMPT" '{content: $m}')" >/dev/null
# The turn is done once the run has new events and is idle again.
answered() { (( $(last_seq "$old_run" "$seq") > seq )) && run_idle "$old_run"; }
wait_for "the run on the draining pod to finish a turn" answered
pod_terminating "$old_pod" || fail "$old_pod stopped draining before the turn"
api DELETE "/api/v1/runs/$old_run" >/dev/null
run_ended() { [[ $(run_field "$old_run" state) != idle ]]; }
wait_for "the old pod's run to end" run_ended
state=$(run_field "$old_run" state)
[[ $state == terminated ]] || fail "the old pod's run ended $state, want terminated"
echo "PASS 3: run $old_run finished a turn on the draining $old_pod and ended $state"

echo "== Back to the floor"
api DELETE "/api/v1/runs/$new_run" >/dev/null
old_pod_gone() { ! k get pod "$old_pod" >/dev/null 2>&1; }
wait_for "$old_pod to exit" old_pod_gone
wait_for "the agent to return to 1 pod" one_live_ready_pod
[[ $(pod_template "$(live_pods)") == "$new_template" ]] || fail "the agent's pod is not on the new template"
echo "PASS 4: the agent is back to 1 pod, $(live_pods) on template $new_template"

echo "== Model change"
model_pod=$(live_pods)
api PATCH "/api/v1/agent-types/$agent" -d "$(jq -n --arg m "$MODEL2" '{model_id: $m}')" >/dev/null
# An empty pod drains at once, so it can be gone before a poll sees it terminating.
pod_leaving() { pod_terminating "$1" || ! k get pod "$1" >/dev/null 2>&1; }
wait_for "the model change to drain $model_pod" pod_leaving "$model_pod"
replaced() { one_live_ready_pod && [[ $(live_pods) != "$model_pod" ]]; }
replaced || fail "$model_pod started terminating with no Ready replacement"
echo "PASS 5: the model change bound $(live_pods) before it drained $model_pod"

echo "pod events (type, name, Ready, deletion):"
sed 's/^/  /' "$pods_log"
