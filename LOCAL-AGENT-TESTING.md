# Testing the Agent Control Plane Locally

## Option 1: Unit tests (no cluster, no `claude` binary required)

The transform layer has a full unit test suite against in-memory fixtures:

```bash
cd backend && go test ./internal/agents/sessionmanager/... -v
```

Exercises the full event pipeline: text deltas, tool use/result, `message_complete`, error handling, MCP config writing. No subprocess, no Postgres, no `claude` binary needed.

---

## Option 2: Run the session-manager directly

The session-manager is a plain Go HTTP server. You can run it standalone without the k3d cluster.

**Prerequisites:**
- `make dev-up` (Postgres running)
- `claude` CLI installed and authenticated (`claude --version` to verify)

**Start the server:**

```bash
# From repo root
cd backend && go run ./cmd/session-manager/main.go
```

`local.yaml` sets `claude.bare: false` — OAuth is used instead of `ANTHROPIC_API_KEY`. The server starts on `:8090`.

The workspace directory (`/workspaces`) must exist or you need to override it. Use `/tmp/workspaces` for local testing:

```bash
# Create a local.yaml override (or set the env var directly)
WORKSPACES_BASE_PATH=/tmp/workspaces cd backend && go run ./cmd/session-manager/main.go
```

**Send a test session:**

```bash
# 1. Create a session
curl -s -X POST localhost:8090/session \
  -H 'Content-Type: application/json' \
  -d '{"run_id":"test-1","agent_type_id":"code","tenant_id":"local"}'

# 2. Send a message (returns 202 immediately; response arrives async)
curl -s -X POST localhost:8090/session/test-1/messages \
  -H 'Content-Type: application/json' \
  -d '{"content":"What is 2 + 2? Answer briefly."}'

# 3. Watch events land in Postgres (repeat until you see message_complete)
psql $DATABASE_URL -c \
  "SELECT event_type, payload FROM run_events WHERE run_id = 'test-1' ORDER BY sequence_no;"

# 4. Close the session
curl -s -X DELETE localhost:8090/session/test-1
```

**Expected event sequence in `run_events`:**
```
text_delta       {"delta": "4"}
message_complete {"text": "4"}
message_stop     {}
```

**Note:** The session-manager will attempt to notify `orbit-agents` at the URL in `base.yaml` when events land. That call will fail (404) locally, but it is non-fatal — events still write to `run_events` correctly.

---

## Option 3: Full cluster

The full agent control plane (orbit-agents, StatefulSets, Kubernetes-based session routing) requires the k3d cluster. See `deploy/local/README.md` for the annotated setup sequence:

```bash
make dev-cluster       # Docker Compose + k3d cluster
make cluster-operator  # operator + platform services
make cluster-deploy    # service images + fixtures
```
