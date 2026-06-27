# Orbit context for discovery

Read this before ideating. Discovery that ignores platform reality produces unbuildable
fantasy or reinvents what already ships.

## What Orbit is

**The control plane for AI agents** — the agentic engineering platform that closes the gap
between an agent on a developer's laptop and a secure, governed agent in production. It is
*not* an agent framework, *not* a model router, *not* an LLM wrapper.

## The three questions (the founding spine — a coherence test, NOT a ceiling)

The three questions are *why Orbit exists* — the production-agent problem it was built to
answer, and the source of its credibility (they give a CSO's "how do we know the agent isn't
going rogue?" a concrete answer). Treat them as the **coherence test and the security
guarantees**, not as a gate that every new idea must map onto. **The product already extends
well past them** (see the pillar taxonomy below — Playground, Context Lake, Inference/cost,
Enablement are about developer experience, agent capability, cost, and the commercial
blueprint, not Who/What/How). Discovery is *expected* to open ground beyond the three
questions.

1. **Who is this agent?** Machine-verifiable identity at deploy time. Humans and agents share
   one identity model; a person delegates to an agent with defined scope and full
   accountability, and the agent can never access more than the delegator.
2. **What can it access?** Every action checked against policy *before* it executes. Admins
   define what each agent can do down to the individual tool; everything else is rejected and
   logged. Enforced at the edge inside each pod — the agent is never trusted.
3. **How does it connect?** Agents never handle credentials. The platform intercepts
   outbound calls and injects credentials just-in-time, deferring to the upstream system as
   the authority. A Claude Code / Codex instance drops in without changing its auth handling.

### How to use this for a candidate pillar

A strong pillar does **at least one** of:
- **(a) deepens** one of the three questions, **or**
- **(b) extends** an existing product pillar (taxonomy below), **or**
- **(c) opens a coherent *new* pillar** — net-new product ground that still fits the thesis
  (governed, production-grade agentic engineering for the dual user). This is allowed and wanted.

The **one hard constraint** ("weaken none", narrowed): it must **not undermine the security
guarantees** that make Orbit credible — verifiable identity, pre-execution policy enforcement,
no-credentials-to-agents, and full traceability to the initiating human. Growth is open;
eroding those guarantees is not. The STRESS phase checks *this*, not "does it map to Who/What/How".

## Product pillars (the real taxonomy — map candidates to these, or propose a new one)

From the Orbit hub. Each pillar owns its capability end to end, console surfaces included.
A discovery candidate should name which pillar it extends — or argue it's a *new* pillar.

| Pillar | What it gives the user (human + agent) |
|---|---|
| **Agents** | Scaling governed agent use cases — security/rigor handled once by the platform, not rebuilt per team. |
| **Identity & Policy** | Who humans and agents are and what they're allowed to do, under one permission model. |
| **Inference & Model Gateway** | How humans and agents reach models — one gateway controlling availability and cost. |
| **Audit & Governance** | The record of everything humans and agents do, traced to who initiated it and what allowed it. |
| **Playground** | (Forming) A place to experiment with / demo agents on the platform. |
| **Context Lake** | (Forming) Context/memory substrate agents draw on. |
| **Infrastructure & Deployment** | A simple-to-consume but opinionated interface for deploying Orbit into an enterprise's existing infra. |
| **Enablement** | How Orbit is used internally, demoed to customers, and reused as blueprints across engagements. |
| **Cross-pillar** | Initiatives spanning multiple pillars. |

Roadmap horizons: **Shipped · Now · Next · Later**. (Source: Orbit Initiatives DB.)

## Strategic direction — the north stars (bias discovery toward these)

Where the platform is *heading*. Pillars and the current codebase are the floor, not the goal —
discovery should pull toward this vision, not just extend what exists. Weight opportunities that
advance one of these (and ideally point past today's feature set):

- **★ Governed autonomous operations ("dark factories").** The platform should make *lights-out*,
  fully autonomous agent operations possible — fleets of agents running real enterprise work
  (software factories, ops, remediation) — but **governed, fully audited, and accountable** the
  whole way. The differentiator is "autonomy *and* governance," not one or the other. Favor
  opportunities that raise the **autonomy × scale** of agent operations while keeping the security
  guarantees intact. (This is the unlock no agent-framework or IAM vendor is positioned to deliver.)
- **★ Human acceleration / net-new capability.** Features that let a human do something they
  **couldn't do before**, or that was **painfully hard** — a superpower, not just an admin control.
  Discovery skews toward governance/controls (the security lane); deliberately counter-weight
  toward operator/developer *leverage* and capability, where the human is accelerated by agents.

When scanning, treat these as the destination: ask "what would move Orbit toward governed dark
factories?" and "what would give a human a capability they don't have today?" — then research the
world for what's newly possible that serves them.

## Commercial frame (affects what "valuable" means)

Orbit is an **enablement model, not a licensed product** — ClearRoute does not sell it; it's
the **reference implementation / blueprint** ("art of the possible") that helps clients build
their own version. It runs as a product inside ClearRoute's Engineering Capability.
Showcasing at **PlatformCon London, June 2026**; partial open-source under evaluation (decision
targeted H2 2026 — an option, not a commitment). So a pillar's value can also be: *does it
strengthen the blueprint/demo/enablement story for clients?* — a legitimate third value axis
alongside human-operator value and agent-consumer value.

## The dual user (critical for this skill)

Orbit's "user" is **two populations at once**, and good pillars serve both:

- **Humans** — platform admins, security teams, developers deploying/operating agents. They
  want governance, visibility, trust, and low friction to ship.
- **Agents** — autonomous agents are *first-class consumers* of the platform, not just
  subjects of it. They "use" Orbit's identity, policy, inference gateway, and (designed)
  credential/tool gateways. Ask: what does the *agent* get — fewer auth headaches, scoped
  access it can rely on, tools it can call safely?

A pillar that only serves one population is half a pillar. Always frame both.

## What Orbit is NOT (Blue-Ocean guardrails)

Differentiation comes from *not* drifting into these. If a pillar starts to look like one of
these, that's a signal to redesign or to articulate the sharp difference:

- Not an **agent framework** (LangChain/CrewAI/AutoGen) — Orbit governs agents, doesn't build them.
- Not a **model router / LLM gateway as the product** — the inference gateway is a means
  (governance + cost + identity), not the point.
- Not an **LLM wrapper** or a chat product.

## Underlying stack (the "Closed World" resources)

Established platform engineering applied to a new domain — prefer ideas built from these:

- **SPIFFE** workload attestation (identity)
- **OPA** policy-as-code — PDP/PAP + PEP (authorization)
- **Istio / Envoy** at the edge; the **identity-proxy** ext_proc sidecar (enforcement + credential injection)
- **Kubernetes-native**, standards-based; the **operator** reconciles Tenant/Platform CRDs
- **OpenTelemetry** for observability; every action traces to the human who initiated it and the policy that authorized it

## Built vs. designed-but-not-built (verify before relying)

State as of this skill's authoring — **confirm against the code/docs before depending on it**.

**Built and running** (13-binary control plane):
- identity / auth (`cmd/identity`, port 8080) — identity, sessions, PATs, tenant registry
- identity-proxy sidecar (`cmd/identity-proxy`)
- policy enforcement — OPA PDP/PAP (`cmd/policy-service`, 8083) + PEP (`cmd/policy-gateway`, 8084)
- inference gateway (`cmd/inference-gateway`, 8081)
- agent run control plane (`cmd/orbit-agents`) — runs/sessions/sandboxing + the sandbox/exec stack
- session-manager (`cmd/session-manager`, 8090)
- Kubernetes operator (`cmd/operator`)

- **credential broker — built as a pattern** (`internal/credentials/`): adapter model with a
  **GitHub App** adapter and a **passthrough** adapter, a **k8s-file** resolver, repository +
  configmap config. Plus **egress control** (OPA `policyservice/bundle/authz_egress`) and the
  **GitHub connector** in `internal/platform/connections/`. So "How does it connect?" is real,
  not a stub — the open ground is *breadth* (more connectors/upstreams) and generalization, not
  the core pattern.

**Designed but NOT yet built** (genuine empty stubs — fertile greenfield for discovery):
- the governed **MCP / tool gateway** (`internal/toolgateway/` — empty) — the "What can it
  access?" pillar extended to *tools*.
- a platform-wide **event spine** (`internal/events/` — empty) — the "audit everything" backbone.

These two stubs are committed direction without committed design — strong pillar ground. But
**verify current state against the code before relying** — this list goes stale (the credential
broker did). Grep `backend/internal/` rather than trusting this note.

## Where discovery outputs go

- Research dossiers → `docs/strategy/research/<topic>-dossier.md`
- Pillar / product-fit docs → `docs/strategy/pillars/<pillar>.md`
- PRDs → `docs/prd/YYYY-MM-DD-<slug>.md` (conform to `docs/prd/template.md`)
- Index → `docs/strategy/README.md`

## Key source docs to consult

- `AGENTS.md` (root) — conventions, the three questions, PRD→ADR workflow
- `docs/architecture.md` and `docs/adr/README.md` — current architecture + decisions
- `docs/code-organization.md` — binary/port map
- `docs/prd/` — existing PRDs (read 1–2 for tone/depth before writing new ones)
