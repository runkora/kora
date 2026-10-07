# Kubernetes-Only Local Development

**Status:** Accepted
**Date:** 2026-07-10
**Deciders:** Justin Wilkin
**Technical Story:** Supersedes [local-development-environment](2026-03-10-local-development-environment.md). Collapses the two-tier local environment into one Kubernetes-native path and reduces the day-one command surface.

---

## Context

[local-development-environment](2026-03-10-local-development-environment.md) established two tiers: a host process for daily Go work (`make dev`), and a k3d cluster for anything touching SPIFFE, Istio, or Envoy. That split made sense when the platform was mostly an HTTP API with a database behind it.

It no longer describes the platform. kora's behaviour now lives in the parts that Tier 1 cannot run:

- **Identity.** SVID exchange and workload attestation come from SPIRE. There is no SPIRE on the host.
- **Policy.** Enforcement happens at the edge, in Envoy with OPA, before a request reaches application code. On the host, that layer is absent, so a request takes a different path through the system than it takes anywhere else.
- **Credentials.** The `identity-proxy` sidecar and `egress-gateway` inject credentials into outbound calls. Neither exists on the host.
- **Agents.** `orbit-agents`, `session-manager` and `sandbox-exec` orchestrate pods. They have no meaning outside a cluster.

`make dev` starts the `identity` binary and nothing else (`Makefile:762`). It does not start the inference gateway, the policy service, the policy gateway, the tool gateway, or the agent control plane. An engineer running Tier 1 is exercising one service with its enforcement layer removed.

### Problem Statement

The two-tier model asks every engineer to hold two environments in their head, decide which one a given change needs, and remember a different command sequence for each. The cheaper tier does not exercise the platform's defining behaviour, so work that passes there routinely fails in the cluster. Onboarding pays this cost on day one, before anyone has the context to reason about it.

### Goals

- One way to run kora locally.
- A day-one path that is a short, ordered list of commands with no branching.
- Composed recipes: the granular targets stay, and the common paths call them.
- The environment an engineer develops against behaves like the one their code ships to.

### Non-Goals

- Changing CI, staging, or production topology.
- Removing the granular `cluster-*` and `redeploy-*` targets. They remain the building blocks.
- Preserving a host-process fast loop. That is the cost of this decision, taken deliberately.

---

## Decision Drivers

- Correctness of the local environment matters more than the speed of the inner loop, because the failures Tier 1 hides are identity, policy and credential failures, which are the product.
- Every branch in an onboarding path is a place a new engineer stalls.
- The platform is Kubernetes-native by design. A local environment that pretends otherwise teaches the wrong model.

---

## Options Considered

### Option 1: Keep the two-tier model

Cheapest. Keeps the fast loop. Rejected because it preserves the thing being fixed: two environments, a branch on day one, and a cheap tier whose passing signal means little.

### Option 2: Kubernetes-only, redeploy as the inner loop

Delete `make dev` and the host frontend server. Every change is built into an image and rolled into the cluster. One environment, one mental model, no branch.

The cost is real: the inner loop becomes a docker build, a registry push, and a rollout, in place of a sub-second `air` restart.

### Option 3: Kubernetes-only, with a traffic-intercept tool

As Option 2, but add mirrord or Telepresence so a locally-built binary intercepts an in-cluster pod's traffic. Recovers much of the speed and keeps cluster semantics.

Rejected for now. It reintroduces a second way to run a service, plus a dependency and a concept to teach, in exchange for speed we have not yet measured as a blocker. Revisit if the redeploy loop proves to be the constraint in practice.

---

## Decision

**Local development runs in Kubernetes. There is no host-process path.**

`make dev`, `make frontend-dev`, `air` and `.air.toml` are removed. The backend inner loop is `make redeploy SERVICE=<name>`. The frontend inner loop is the same, against the `frontend` service.

The granular recipes stay. Three composed targets sit on top of them:

| Target | What it does | When |
|---|---|---|
| `make setup` | Installs tooling, git hooks, local DNS, and mkcert TLS. Scaffolds `.env`. | Once per machine |
| `make up` | Backing services, k3d cluster, operator (SPIRE, Istio, kora services), secrets, tenants, workloads. Idempotent. | Start of a session, and any time the cluster drifts |
| `make down` | Tears the cluster down. | End of a session |
| `make redeploy` | Rebuilds and rolls every service. `SERVICE=<name>` narrows it to one. | Inner loop |
| `make auth` | Device Authorization Grant against the cluster. Writes the token pair. | When the token expires |

`make up` composes `dev-up`, `dev-cluster`, `cluster-operator` and `cluster-deploy` in order. Those targets remain callable on their own for anyone who wants to drive a single step.

Postgres, Redis and Dex stay in Docker Compose. They are dependencies of the cluster, not a development tier, and `make up` starts them.

---

## Environment configuration

`.env` currently holds three kinds of value, and the first kind dies with this decision.

**Host-process config.** `ORBIT_API_URL`, `INFERENCE_GATEWAY_URL`, `POLICY_GATEWAY_URL`, `POLICY_SERVICE_URL`, `TOOL_GATEWAY_URL`, `ORBIT_AGENTS_URL`, `APP_ENV`, `DB_HOST`, `REDIS_HOST`, `ORBIT_ACCESS_TOKEN_TTL_MINUTES`, `ORBIT_TENANTS_CONFIG`, `ORBIT_TOKEN_JWKS_URL`, `ORBIT_TOKEN_SIGNING_KEY_FILE`, `VERTEX_ANTHROPIC_LOCATION`. These are read by a backend or frontend process running on the host. In the cluster the same settings come from chart values (`deploy/local/orbit-values.yaml` is what sets the local 4h token TTL, not `.env`). They are removed.

`NEXT_PUBLIC_OIDC_REDIRECT_URI` has no consumer anywhere in the tree. It is removed.

**Compose inputs.** `DB_USER`, `DB_NAME`, `DB_PORT`, `DB_PASSWORD`, `REDIS_PORT`, `REDIS_PASSWORD`, `GLAUTH_DEV_PASSWORD_HASH`. These stay, but as defaults inside `docker-compose.yml` (`${DB_PORT:-5432}`) rather than as lines a human must copy.

**Cluster secret inputs.** `create-secrets.sh` reads `BFF_SESSION_SECRET`, `DB_PASSWORD`, `REDIS_PASSWORD` and the tenant OIDC client secret from `.env` and writes them into `orbit-secrets`. These stay, and are generated.

### How `.env` gets built

`.env` becomes a generated, machine-local file, not a template you copy and edit. `make setup` writes it if it is absent and never overwrites it. Values with a safe default do not appear in it. Secrets are generated with `openssl rand`. Nothing requires editing before `make up` works.

`.env.example` stops being the source of the real file and becomes documentation of the optional overrides: `AWS_PROFILE` and `BEDROCK_ENABLED` for Bedrock, `K3D_FIX_DNS` for Docker Desktop.

Postgres and Redis passwords stay at fixed local defaults rather than being randomized per machine. The Postgres volume persists across `make down`, and the password is fixed at first initialization, so a rotating password would silently break every existing volume. These services listen on loopback in a throwaway cluster and the password protects nothing.

`BFF_SESSION_SECRET` and the tenant OIDC client secret are generated per machine, because they are real secrets and nothing outside `.env` depends on their value.

---

## Consequences

### Positive

- One environment. A change that works locally has been through the mesh, the sidecar, the edge OPA, and the real identity flow.
- Day one is `make setup`, `make up`, `make auth`. No tier to choose.
- The signal from local testing is worth something, because it is the same path as production.
- `make test-integration` stops being a special case, because a cluster is always present.

### Negative

- **The inner loop gets slower.** Every backend change is a docker build, a push, and a rollout, where it used to be an `air` restart. This is the price of the decision and should be stated plainly to anyone joining.
- **The frontend loses hot module reload.** Frontend work goes through `make redeploy SERVICE=frontend`. This is the sharpest edge of the decision and the most likely thing to be revisited.
- Kubernetes is now a hard prerequisite. An engineer cannot do useful work without a cluster running, and the machine must have the memory for it.
- Unit tests still run on the host via `make check`, so not every change needs the cluster. That remains the fastest feedback available.

### Mitigations

- `make redeploy-warm` prebuilds every image so BuildKit caches are hot before the first real change.
- `make redeploy SERVICE=<name>` touches one service, not the platform.
- If the loop proves to be the binding constraint, Option 3 is the escape hatch, and this ADR should be revisited rather than the two-tier model reinstated.

---

## Follow-ups

- Remove `make dev`, `make frontend-dev`, `.air.toml`, and the `air` install from `make install-tools`.
- Add `make up`, `make down`, `make auth`; make `SERVICE` optional on `make redeploy`.
- Strip the host-process variables from `.env.example`, move compose inputs to defaults in `docker-compose.yml`, and have `make setup` generate `.env` with the two real secrets.
- Rewrite `docs/guides/day-one.md` and `docs/guides/local-development.md` around the single path, and delete the Tier 1 and Tier 2 language from `README.md`, `CONTRIBUTING.md` and `AGENTS.md`.
- Mark [local-development-environment](2026-03-10-local-development-environment.md) as superseded once this is accepted.
