---
name: verify-prs
description: >-
  Triage, locally verify, and drive open GitHub PRs (especially agent/bot-authored
  ones) toward merge in the kora repo. Inventory + audit the queue, deploy each branch
  to the local k3d cluster and exercise REAL behaviour (not just CI), fix
  lint/tests/review-comments, then merge / close-as-duplicate / send back with documented
  findings. Use when asked to review, triage, "babysit", or shepherd open PRs, to "get PRs
  merged", to verify a PR locally, or to process the PR backlog.
---

# Verify open PRs and drive them to merge

A repeatable workflow for taking kora's open PRs — most authored by the **remote coder
agents** from thin specs — to `merged`, `closed`, or `back-to-author` with evidence. The
defining principle:

> **CI green ≠ feature works.** Agent PRs pass lint/build/unit-tests yet are routinely
> non-functional end-to-end (registered-but-unroutable, breaks an existing path, a runtime
> SQL error a unit test never hits). The value here is **deploying each branch to the
> integrated cluster and exercising the real behaviour** — that is what catches what CI,
> static review, and even the bot's own review miss.

---

## Before you start

1. **Confirm scope.** Which PRs? Often **agent-authored only** — leave human PRs unless
   asked. The user owns the final call on what merges.
2. **Know your environment — it sets how far you can verify:**
   - **Local dev machine (full):** you have the k3d cluster, docker-compose infra, and a
     GUI. Do the complete runtime + UI verification in Phase 3.
   - **Headless / remote agent (partial):** no cluster or browser. Verify via build + tests
     + the actual API/BFF contracts + tracing the code path, and **explicitly flag in your
     report what could NOT be runtime-verified** ("UI not visually confirmed; endpoint
     traced in code only"). Don't fake a cluster, and don't run the GUI/redeploy steps.
3. **Guardrails — confirm with the user before any outward / hard-to-reverse action:**
   - **Merging or closing a PR** is the human's decision. Pushing *fix* commits to a PR
     branch is fine and expected; merging/closing is not — propose it, don't do it unasked.
   - **Rebuilding the cluster** tears down the running one (~20-40 min) — confirm first.
4. **Read** `AGENTS.md` + `docs/contributing/local-development.md` for conventions and make targets.
5. **Switch kube context before any redeploy** — `make redeploy-*` uses the *default*
   context, so targeting EKS dev by mistake is the #1 footgun (ArgoCD then masks it as a
   no-op): `kubectl config use-context k3d-orbit-local`.
6. `mkdir -p .scratch/shots` — gitignored workspace for the audit doc + screenshots. Write
   findings here as you go; never trust your memory of a 15-PR queue.

**Match verification depth to risk.** A formatting-only fix needs build + tests (Phase 4),
not a deploy. A new endpoint / feature / migration needs runtime verification (Phase 3).
Redeploys and rebuilds take minutes — **run them in the background and poll the rollout**,
and **batch by service** to avoid repeat builds.

---

## Phase 1 — Inventory

```bash
gh pr list --state open --limit 100 \
  --json number,title,author,headRefName,baseRefName,createdAt,updatedAt,isDraft,mergeable,changedFiles,additions,deletions \
  --jq 'sort_by(.number)'
# Per-PR CI + merge state:
for pr in <numbers>; do
  gh pr view $pr --json mergeable,mergeStateStatus,statusCheckRollup \
    --jq '{n:'"$pr"', mergeable, mergeStateStatus, checks:[.statusCheckRollup[]?|{name:(.name//.context),conclusion}]}'
done
```
`mergeStateStatus: BLOCKED` usually means *awaiting review/CI*, not a conflict — conflicts
show as `DIRTY`/`CONFLICTING`. Bucket by author (bot vs human).

## Phase 2 — Audit (per PR → `.scratch/<audit>.md`)

One section per PR: **claims · what it changes · CI/merge state (root-caused) · cross-PR
links · verdict + evidence.** Gather: body (`gh pr view N --json body`); files
(`gh pr diff N --name-only`, skim `gh pr diff N`); **CI failures root-caused**
(`gh pr checks N`, then `gh run view <run-id> --log-failed` — lint? real build break? flaky
infra?); rebase need; and **cross-PR relationships** (duplicates, same-file overlaps,
stacked branches). For a large queue, fan the reads out with parallel subagents — but **you**
verify anything that gates a merge.

Common signals: a fast-failing (<15s) Docker bake job = flaky registry pull (re-run);
near-identical titles = duplicates (keep one); branch `>10` commits behind main = rebase +
may predate behaviour the catalog/fixtures now rely on.

## Phase 3 — Local verification *(local dev machine; skip if headless — see "Before you start")*

### 3a. Cluster healthy?
```bash
kubectl --context k3d-orbit-local get pods -A | grep -vE 'Running|Completed'
```
If pods CrashLoop after a Colima/k3d restart (DNS `no such host` for `orbit-postgres`, or
SPIRE `Trust Bundle and Server don't agree`), **rebuild — don't debug Istio/SPIRE** (confirm
with the user first; ~20-40 min):
```bash
make dev-cluster-down && make dev-cluster && make cluster-platform && make cluster-deploy
```
Infra (Postgres/Redis/Dex) runs in docker-compose; k3d services reach it over the shared
network. `make cluster-restore` only re-pushes images after a restart — it won't fix DNS/SPIRE.

### 3b. Authenticate
```bash
make auth-device-k3d     # device grant → browser → dev@orbit.local.clearroute.io / password
                         # token → /tmp/orbit_credentials.json
make auth-refresh-k3d    # tokens expire (~15 min); refresh, don't re-auth, while iterating
```
Call APIs by reading the token inline (it is NOT in the env), through a port-forward:
```bash
kubectl port-forward svc/orbit-identity -n orbit-system 6069:80 &
kubectl port-forward svc/orbit-inference-gateway -n orbit-system 8081:80 &
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/v1/models
```
Re-establish port-forwards after every pod restart (a dropped forward shows as `HTTP 000`).

### 3c. Deploy the branch and exercise it
```bash
git checkout -B verify/prNN origin/<headRefName>     # redeploy builds from the working tree
make redeploy SERVICE=identity                       # background it + poll the rollout.
                                                     # services: identity, inference-gateway,
                                                     # policy-service, agents, frontend, … (make redeploy-list)
```
Then **prove behaviour**, not just that it built:
- **Backend** — capture a *baseline on main first* where it helps (an endpoint that should
  now behave differently), then deploy the branch and hit the real endpoint with curl; read
  pod logs (`kubectl -n orbit-system logs <pod> -c orbit-<svc>`) to confirm the code path,
  region/endpoint, error codes.
- **Frontend** — served at `https://orbit.local.clearroute.io` (an HAProxy container maps host 443 to the k3d load balancer;
  mkcert cert trusted). Drive Safari + screenshot (a locked Private window blocks scripting;
  `open location` opens a normal one), then `Read` the PNG:
  ```bash
  osascript -e 'tell application "Safari" to open location "https://orbit.local.clearroute.io/<route>"'
  WID=$(osascript -e 'tell application "Safari" to id of front window')
  screencapture -x -o -l "$WID" .scratch/shots/prNN-<view>.png
  ```
- **Need data?** Seed it directly in Postgres
  (`docker exec orbit-postgres psql -U orbit -d orbit -c "…"`). Read the query/handler first
  so rows match how it's read (e.g. usage keyed by the agent SPIFFE principal
  `spiffe://{trust-domain}/tenant/orbit-tenant-{tenant}/agent/{slug}`; deliveries that satisfy the `WHERE`/`JOIN`). Note
  what you seeded — harmless in dev.

**Restore the cluster** when done: redeploy the affected services from an **up-to-date local
main** (`git fetch origin main && git switch main && git merge --ff-only origin/main` — a
*stale local main redeploys old code*), and reset any catalog/config you mutated.

## Phase 4 — Fix-forward (the easy wins)

Sound feature, mechanical blocker → fix on the branch and push:
- **Lint** — most "failures" are formatting. Run the *full* linter, not just `gofmt`
  (`cd backend && golangci-lint run ./...` — `govet: enable-all` catches shadow/misspell);
  frontend `pnpm lint && pnpm tsc --noEmit`. **The static/CI summary undercounts lint** — verify yourself.
- **Missing tests** — extract inline logic into a pure helper and unit-test it (vitest /
  Go table tests) rather than a fragile full-render test. Tests are mandated for new code.
- **Flaky CI** — `gh run rerun <run-id> --failed`.
- **Commit discipline — the `make check` pre-commit hook reformats AFTER staging.** A
  committed file can be unformatted while your local tree is clean, so CI's `prettier --check`
  / `gofmt -l` fails (`Frontend / Frontend Check`). **Format-then-stage:**
  `cd frontend && pnpm exec prettier --write .` and `cd backend && make fmt`, *then* `git add`.
  After committing, `git status`; if a just-committed file shows modified again, that's the
  post-hook reformat — commit it too. (`go fix` similarly modernises, e.g. `Split`→`SplitSeq`.)
- Push to the **PR's branch** (`git push origin <headRefName>`), then **watch CI to green**
  — `gh pr checks N`; confirm the run's `headSha` matches HEAD (don't trust a stale run).
- No Claude attribution in commit messages.

## Phase 5 — Outcomes (propose; let the user merge/close)

- **MERGE** — green + verified working. Surface it; **get the user's go-ahead before merging.**
- **FIX-THEN-MERGE** — sound feature, mechanical blocker you fixed in Phase 4.
- **NEEDS-WORK** — back to author with a **comment documenting the findings**
  (`gh pr comment N --body-file …`). Use this *even when CI is green* if local verification
  shows the feature is incomplete/broken. Never silence a lint that hides a real gap (e.g. an
  `unused` helper that was meant to enforce a scope).
- **CLOSE** — duplicate (keep the newer/cleaner; close the rest with a pointer) or
  redundant/stacked (commit already merged via another PR — verify first:
  `git show origin/main:<file> | grep …`). **Confirm before closing.**

## Phase 6 — Address review comments

```bash
gh api repos/clear-route/orbit/pulls/N/comments  --jq '.[]|{user:.user.login,path,line,body}'
gh api repos/clear-route/orbit/pulls/N/reviews   --jq '.[]|{user:.user.login,state,body}'
```
**Assess each on its merits against the actual code — the bot's "blockers" are frequently
false positives.** Verify before acting: a "SQL injection" claim where the value is a bind
param (`$2`) is wrong (pgx parameterises); "swallows the error" where the code returns
`fmt.Errorf("…: %w", err)` is wrong; a "wrong envelope" where one endpoint returns the object
directly and another wraps in `{data:…}`. **Fix the real ones, reply explaining the false
ones** (cite exact lines), then re-run CI to green.

---

## Reference — quick command map

| Need | Command |
| --- | --- |
| Switch to local cluster | `kubectl config use-context k3d-orbit-local` |
| Cluster health | `kubectl get pods -A \| grep -vE 'Running\|Completed'` |
| Rebuild cluster (confirm first) | `make dev-cluster-down && make dev-cluster && make cluster-platform && make cluster-deploy` |
| Auth / refresh | `make auth-device-k3d` · `make auth-refresh-k3d` (token: `/tmp/orbit_credentials.json`) |
| Redeploy a service | `make redeploy SERVICE=<svc>` · `make redeploy-frontend` · `make redeploy-list` |
| Postgres | `docker exec orbit-postgres psql -U orbit -d orbit -c "…"` |
| Frontend URL | `https://orbit.local.clearroute.io` |
| CI failure log / re-run | `gh run view <run-id> --log-failed` · `gh run rerun <run-id> --failed` |
| Quality gate | `make check` (backend) · `cd frontend && pnpm lint && pnpm tsc --noEmit` |

## Hard-won gotchas (do not relearn these)

1. **CI green ≠ working** — verify real behaviour before calling an agent PR done.
2. **`make redeploy-*` uses the default kube context** — switch to `k3d-orbit-local` first.
3. **Stale k3d after restart → rebuild, don't debug** (CrashLoop + DNS/SPIRE errors).
4. **Pre-commit hook reformats after staging** → format-then-stage, or commit the reformat.
5. **The static/CI lint count is incomplete** — run the full `golangci-lint` / `tsc` yourself.
6. **Bot review "blockers" are often false positives** — verify each against the code.
7. **Tokens expire (~15 min)** — `make auth-refresh-k3d`; re-establish port-forwards after pod restarts.
8. **A stale local `main` redeploys old code** — `git fetch && merge --ff-only origin/main` before restoring.
9. **Don't merge/close without the user's OK** — propose; pushing fixes to a branch is fine.
