---
name: product-discovery
description: >-
  Run structured-but-emergent product discovery for the Orbit platform: research the
  market and existing products (web search + competitor UI teardowns, screenshots when the
  machine supports it), ideate end-to-end using a library of thinking lenses (Systematic
  Inventive Thinking, Closed World, Jobs-To-Be-Done, Wardley mapping, Blue Ocean / ERRC,
  system dynamics, first principles, inversion/pre-mortem, analogical transfer), synthesize
  candidate roadmap pillars, stress-test them, map their long-term product fit, and write
  one or more PRDs. Use when asked to discover/research/ideate a feature or roadmap pillar,
  do product discovery, brainstorm where the platform should go, build a competitive
  teardown, or turn a vague idea into PRD(s). Works with NO seed: given nothing (or "discover
  something", "surprise me", "find where we should go") it runs autonomous discovery — scans
  Orbit's roadmap + the market, surfaces net-new opportunities itself, and proceeds without
  asking for a topic. Stops at PRDs — no technical architecture/ADR.
---

# Product discovery — emergent thinking → roadmap pillars → PRDs

Take a fuzzy prompt ("should Orbit have X?", "where should the platform go for governed
tool use?", "research the agent-identity space") and drive it, through a structured pipeline
that is deliberately divergent before it converges, to **one or more PRDs** that conform to
`docs/prd/template.md`.

> **The whole point is emergence, and the enemy of emergence is premature convergence.** An
> LLM's default is to collapse to one tidy answer fast. This skill fights that: it forces
> many *independent* lenses over the same problem, keeps them apart long enough to generate
> genuinely different directions, and only then synthesizes the non-obvious overlaps. Run
> the lenses **blind to each other** (fan-out) — that is where ideas appear that no single
> pass would produce.

## The deliverable chain (and where it stops)

```
RESEARCH ──▶ IDEATE ──▶ SYNTHESIZE ──▶ STRESS ──▶ PILLAR-MAP ──▶ PRD(s)
 web +        lenses,     cluster +      pre-mortem  long-term      docs/prd/
 teardowns    blind to    score →        + 2nd-order  product fit    (your template)
 + shots      each other  candidates     effects
```

**It stops at PRDs.** No technical architecture, no ADR, no implementation design. PRDs
describe *what the end user gets and why it is valuable*. The ADR/architecture work is a
separate, later step in `AGENTS.md`'s workflow — do not do it here.

## Two hard rules (every phase obeys these)

1. **Frame everything as end-user value — and the user is dual.** Every pillar, every story,
   every "why" must answer **both**:
   - *What does the **human** platform operator / developer / admin get?*
   - *What does the **agent** consuming Orbit get?* In Orbit's world, autonomous agents are
     first-class **users** of the platform, not just things it governs. A pillar that only
     serves humans, or only serves agents, is half a pillar — say so explicitly.
2. **Stay coherent with the thesis — but don't treat the three questions as a ceiling.** The
   three questions (*Who is this agent? What can it access? How does it connect?*) are Orbit's
   founding spine and the source of its credibility, **not a gate every idea must map onto.**
   The product already extends past them (real pillars: Agents, Identity & Policy, Inference &
   Model Gateway, Audit & Governance, Playground, Context Lake, Infrastructure & Deployment,
   Enablement). A strong candidate does **at least one** of: (a) deepens one of the three
   questions, (b) extends an existing pillar, or (c) **opens a coherent new pillar** — net-new
   ground is wanted. The one hard line ("weaken none", narrowed): it must **not undermine the
   security guarantees** — verifiable identity, pre-execution policy, no-creds-to-agents,
   traceability to the initiating human. Map each candidate to a pillar (or argue it's a new
   one). See `references/orbit-context.md` for the pillar taxonomy, the commercial/enablement
   value axis, and the "Orbit is NOT a…" guardrails.

## Before you start

1. **Detect the intake mode — and never require a seed.**
   - **Seeded** — the user gave a feature idea, problem space, or pillar. Frame around it. Only
     ask a clarifying question if a *given* seed is genuinely ambiguous; never ask just to get
     a topic.
   - **Open / autonomous** — the user gave *nothing* (or said "discover something", "surprise
     me", "find where we should go"). **Do not ask them for a topic.** Generate the opportunity
     surface yourself via the **Opportunity Scan** in Phase 0, pick the richest 1–3 directions,
     and run the pipeline on them. This is a first-class mode, not a fallback.
2. **Read `references/orbit-context.md`** — the three questions, what is built vs. stubbed,
   and what Orbit explicitly is *not*. Discovery that ignores current platform state
   produces unbuildable fantasy or reinvents what exists.
3. **Pick the run mode** (below). Default is **fan-out**.
4. **`mkdir -p .scratch/discovery`** — gitignored workspace for raw lens output, fetched
   pages, and screenshots before you promote the keepers into `docs/strategy/`.

### Persistence model — what's durable vs. ephemeral

Be deliberate about this; don't leave valuable output in `.scratch`.

| Artifact | Home | Lifetime |
|---|---|---|
| **Opportunity slate** → promoted into the **radar** | `docs/strategy/opportunity-radar.md` (single, **append-only**) | Durable — deferred directions are future seeds |
| Research dossier | `docs/strategy/research/<topic>-dossier.md` | Durable |
| Pillar map (product-fit) | `docs/strategy/pillars/<pillar>.md` | Durable |
| PRD(s) | `docs/prd/YYYY-MM-DD-<slug>.md` | Durable |
| Index | `docs/strategy/README.md` | Durable |
| Frames, **raw per-lens ideation logs**, fetched pages, screenshots-in-progress, intermediate candidate notes | `.scratch/discovery/` | **Ephemeral** — process exhaust |

Raw lens logs stay ephemeral on purpose: the valuable bits already flow into the PRD's
"Alternatives Considered" and the pillar map's "Emergent signal" section. Don't commit them.

## Run mode: fan-out (default) vs. solo

**Fan-out (default — this is the emergence engine).** Orchestrate the pipeline with the
`Workflow` tool: each thinking lens runs as an *independent subagent that cannot see the
others*, then a synthesizer combines them. This is what produces emergent results and is
worth the token cost for any real roadmap question. Sketch:

```
phase SCAN        → AUTONOMOUS MODE ONLY: parallel opportunity scouts (Orbit roadmap/repo state · market landscape · agent-as-user gaps) → ranked opportunity slate → pick top 1–3 directions
phase RESEARCH    → per selected direction: compose /deep-research for the fan-out web sweep + (optional) UI teardowns → research-dossier
phase IDEATE      → parallel: ONE agent per framework card in frameworks/, EACH blind to the others, each returns its raw divergent output
phase SYNTHESIZE  → ONE synthesizer: cluster across lenses, score, surface non-obvious overlaps → candidate pillars (value-framed, dual-user, mapped to a product pillar — existing or new)
phase STRESS      → per surviving pillar: pre-mortem + "does this undermine the security guarantees?" + 2nd-order effects (inversion-premortem card)
phase PILLAR-MAP  → per surviving pillar: long-term product-fit doc → docs/strategy/pillars/
phase PRD         → parallel: ONE PRD writer per pillar → docs/prd/ using templates/prd.md
```

Use a barrier only where a stage genuinely needs *all* prior results at once (SYNTHESIZE
needs every lens; STRESS/PILLAR-MAP/PRD pipeline per-pillar). Default to `pipeline()`
elsewhere. Only call `Workflow` when the user has opted into multi-agent orchestration
(they ask for discovery/a workflow, or ultracode is on) — otherwise run **solo**.

**Solo (fallback — cheaper, less emergent).** One agent runs the lenses sequentially. It
*will* contaminate lenses (it remembers what it just thought), so the divergence is weaker.
Mitigate by writing each lens's output to `.scratch/discovery/ideation-<lens>.md` and
deliberately ignoring prior files while working the next lens.

## Phase 0 — FRAME (Closed-World boundary)

Set the boundary before generating anything. Load `frameworks/closed-world.md`.

### 0a. Opportunity Scan — **autonomous mode only** (skip if seeded)

When no seed was given, *you* generate the search space. Don't ask the user; go find it — **out
in the world, not in the codebase.**

> **⚠️ The #1 failure mode of this scan: looking inward.** Starting from `backend/internal/` and
> "extending the feature set" reliably produces incremental, me-too, security-lane ideas (audit
> spine, A2A delegation, MCP gateway — if your slate looks like that, you fell into the trap).
> **Invert the order: research the world first, generate ambitious ideas, *then* check them
> against the platform for feasibility.** The repo is a late filter, never the seed. And do not
> ideate from your own training knowledge — **the opportunity surface MUST come from live
> research** (`WebSearch` / `WebFetch` / `/deep-research`). If you didn't search the web, you
> didn't do this phase.

**Step 1 — Research the world (primary, mandatory).** Cast wide; aim for *what's newly possible*,
not what's already shipping. Run real searches across:
- **Emerging tech & frontier research** — arXiv / new protocols / new product categories / what a
  recent model-capability jump just made feasible. "What can agents do this year that they
  couldn't last year?"
- **Adjacent fields, not just competitors** — dev tooling, CI/CD, RPA, BPO/operations, data
  platforms, security — patterns ripe to transfer into governed agentic engineering.
- **The two north stars** (see `references/orbit-context.md` → Strategic direction) as the lens:
  - *Governed autonomous operations ("dark factories")* — what would let fleets of agents run
    real enterprise work lights-out, **and** keep it governed/audited?
  - *Human acceleration* — what would give an operator/developer a capability they don't have
    today, or kill a painful one? (Counter-weight the skill's pull toward admin/controls.)
- White space — jobs **nobody** serves yet, especially for the *agent-as-user*.

**Step 2 — Generate, ambitiously.** Produce a wide set of directions from the research. Force a
spread across **horizons of ambition** — don't let them all be H1:
- **H1 incremental** (extend an existing pillar), **H2 adjacent** (a new pillar / new job), **H3
  transformative** (moves Orbit visibly toward a north star; would feel bold today).
- **Require ≥1 H3 "big swing" in every slate.** A slate of all-incremental ideas is a failed scan.

**Step 3 — Filter against the platform (late).** *Now* bring in Orbit's state — read
`references/orbit-context.md`, grep `backend/internal/`, skim `docs/prd/` + `docs/adr/`, the
Notion roadmap — to (a) drop anything already built/decided, (b) note feasibility / what it
reuses, (c) sharpen the differentiation. Feasibility filters the list; it does **not** generate it.

**Step 4 — Rate & rank (novelty-weighted).** Score each direction 1–5 on:

| Criterion | Weight |
|---|---|
| **Novelty** (genuinely new vs. me-too / incremental) | **×2** |
| **Emergence / ambition** (opens new ground; ≥H2; serves a north star) | **×2** |
| Dual-user value (human + agent) + blueprint value | ×1 |
| Differentiation (vs. "Orbit is NOT a…" and the crowded field) | ×1 |
| Feasibility / fit with the platform | ×1 |

Novelty and emergence are **double-weighted on purpose** — they are the antidote to the inward
bias. Rank by the weighted score and select the top 1–3 (keeping at least one H2/H3). Note the
scores in the slate so the bias is visible and auditable.

Write the ranked **opportunity slate** to `.scratch/discovery/00-opportunities.md` (full detail,
per-criterion scores, horizon tag, and **sources** for every direction). The top 1–3 become the
frame(s) below.

**Then promote it to the radar (durable):** append a dated section to
`docs/strategy/opportunity-radar.md` with the ranked directions, each marked `Selected` or
`Deferred`, plus the market-scan sources. The radar is **append-only** — don't rewrite prior
runs; before scanning, skim past `Deferred` rows (they may now be ripe). As a direction
graduates later, update its Status in the radar to `Promoted` (→ link the pillar map / PRD).
Tell the user what you picked and why, then continue (don't wait for approval unless they asked you to).

### 0b. Frame each direction (both modes)

For the seed (seeded mode) or each selected opportunity (autonomous mode):

- State the problem in one sentence, then state it *inverted* ("how would we guarantee
  agents stay un-governable?") — inversion seeds better divergence.
- List the **resources already inside Orbit's world** — SPIFFE attestation, OPA PDP/PAP,
  Envoy/Istio edge, the identity-proxy sidecar, the inference gateway, the run control
  plane, the operator, the credential-broker pattern. Closed-World ideation prefers solutions
  built from these.
- Name the dual users in play (which humans? which agents?), the relevant product pillar, and
  the time horizon.
- Write `.scratch/discovery/00-frame.md` (one section per direction). This frame is the shared
  brief every lens agent receives.

## Phase 1 — RESEARCH (map the world)

Goal: a **research dossier** (`templates/research-dossier.md`) that maps the category, the
players, and how they frame the problem — so ideation builds on reality, not vibes.

- **Search-primary.** Use `WebSearch` / `WebFetch`, and for anything deep compose the
  existing **`/deep-research`** skill (fan-out search + adversarial citation verification —
  do not rebuild it). Capture: who plays here, how they position, what jobs they claim to
  do, pricing/packaging signals, and notably *what nobody is doing*.
- **Competitor UI teardowns — attempt, never block.** When *how a competitor lays out a
  feature* would inform the design, try to capture it; otherwise rely on fetched content +
  description. Environment-aware:
  - **Local Mac with GUI:** `focus-app` a browser → navigate → `capture-screen` (or the
    `screen-context`/`list-windows` helpers) → save PNG into `.scratch/discovery/shots/`
    and reference it from the dossier. If any step is unsupported, fall back silently.
  - **Headless / remote agent:** skip screenshots; use `WebFetch` page content and *note in
    the dossier that UI was not visually captured*. Never fake a screenshot.
- **Technical-feasibility sanity (light).** When an idea leans on a specific
  library/protocol/API, use the `context7` MCP for current docs so "system design" framing
  stays grounded. Keep it light — this phase is discovery, not architecture.
- Promote the finished dossier to `docs/strategy/research/<topic>-dossier.md`.

## Phase 2 — IDEATE (diverge — do NOT converge here)

Run the lenses. In fan-out, one subagent per card, **blind to each other**; in solo,
sequentially with the contamination guard above. Each lens agent gets: the Phase 0 frame,
the dossier, its single card from `frameworks/`, and `templates/ideation-log.md`.

Pick lenses to fit the question (don't always run all nine — but run at least 4–5 diverse
ones for a real pillar question):

| Lens (card) | Forces you to… | Best for |
|---|---|---|
| `systematic-inventive-thinking.md` | Subtract / divide / multiply / unify / make-dependent existing components | Buildable, constrained-creative ideas |
| `closed-world.md` | Solve using only resources already in Orbit's world | Ideas that ship without new primitives |
| `jobs-to-be-done.md` | Anchor to the *job* a human **and** an agent is hiring Orbit to do | Value framing, dual-user |
| `wardley-mapping.md` | Position capabilities on value-chain × evolution | **Roadmap shape**, what to invest in |
| `blue-ocean-errc.md` | Eliminate / Reduce / Raise / Create vs. the field | Differentiation vs. "Orbit is NOT a…" |
| `system-dynamics.md` | Find feedback loops, leverage points, 2nd-order effects | Compounding-adoption pillars |
| `first-principles.md` | Rebuild from physics, ignore how it's done today | Breaking category assumptions |
| `inversion-premortem.md` | Ask how it fails / how to cause the problem | Stress phase + sharper divergence |
| `analogical-transfer.md` | Steal patterns from k8s RBAC, IAM, service mesh, CI/CD | Mature-pattern reuse |

**Output is raw and plural.** Each lens returns *many* candidate directions, not a
recommendation. Resist scoring or merging now.

## Phase 3 — SYNTHESIZE (converge — find the emergent overlaps)

One synthesizer reads *all* lens outputs together (barrier). Its job is not to average them:

- Cluster the divergent ideas; explicitly hunt for **directions that two+ independent lenses
  converged on from different angles** — those are the emergent signals, flag them.
- Form **candidate pillars** (each is a durable, multi-feature value theme, not a feature).
  For each: the dual-user value (and, where relevant, the enablement/blueprint value), the
  **product pillar it extends or the new pillar it opens** (taxonomy in `references/orbit-context.md`),
  and a rough now/next/later.
- Score against the **novelty-weighted rubric** (same as Phase 0a Step 4: novelty ×2, emergence/
  ambition ×2, dual-user + blueprint value, differentiation, feasibility) plus the hard line
  "does it undermine the security guarantees?" (net-new ground beyond the three questions is
  fine). Keep at least one H2/H3 "big swing"; don't let synthesis quietly converge on the safe
  incremental cluster. Drop the weak ones, keep 2–5.
- Write candidates to `.scratch/discovery/03-candidates.md`.

## Phase 4 — STRESS (adversarial)

For each surviving pillar, load `frameworks/inversion-premortem.md`:
- **Pre-mortem:** it's 18 months later and this pillar failed — why? Write the top 3 causes.
- **Security-guarantees check:** does it erode verifiable identity / pre-execution policy /
  no-creds-to-agents / traceability? If yes, redesign or drop. (Opening ground beyond the three
  questions is fine; weakening a guarantee is not.)
- **Second-order effects:** what does it incentivize? What loop does it create or break?
- Record survivors + their mitigations.

## Phase 5 — PILLAR-MAP (long-term product fit)

For each survivor, write a pillar doc with `templates/pillar-map.md` →
`docs/strategy/pillars/<pillar>.md`: where it sits in the platform long-term, what existing
capabilities it extends or what new pillar it creates, dependencies/sequencing, and the
features that ladder up to it. This is the *strategy* artifact; the PRD is the *spec*.

## Phase 6 — PRD(s)

For each pillar (or each shippable feature within it), write a PRD with `templates/prd.md`
(which mirrors `docs/prd/template.md`) into **`docs/prd/`**, named `YYYY-MM-DD-<slug>.md`
using today's date from session context. The PRD must:
- Lead with end-user value, dual-user **User Stories** (`As a human operator…` *and*
  `As an autonomous agent consuming Orbit…`).
- Name the **product pillar** it extends (or the new pillar it opens) in the Executive Summary,
  and note its relationship to the three questions where relevant — without forcing a fit.
- Stay **out of architecture** — no schemas, no service design, no ADR content. "Proposed
  Solution" stays conceptual / UX-level as the template intends.

Then update `docs/strategy/README.md` index and tell the user what landed where, what the
emergent signals were, and what the recommended next pillar to PRD is.

## Guardrails

- **Outward/destructive actions need confirmation.** Publishing to Notion/Slack, or anything
  outside the repo, is the human's call — propose, don't do it unasked. Writing docs into the
  repo working tree is fine and expected.
- **Don't invent platform state.** If unsure whether something is built, say so or check the
  code — `references/orbit-context.md` lists built-vs-stubbed, but verify before relying.
- **Never fake research or screenshots.** Cite sources in the dossier; flag what couldn't be
  verified or visually captured.
- **Match depth to the ask.** "Quick: is X worth it?" → light research + 3 lenses + one PRD.
  "Where should the platform go?" → full fan-out, all relevant lenses, multiple pillars.
