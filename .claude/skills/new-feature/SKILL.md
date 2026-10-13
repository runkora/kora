---
name: new-feature
description: >-
  Kick off a new kora feature the disciplined way — a guided, planning-only workflow that
  produces an approved PRD and an approved, indexed ADR, ready to hand off to implementation.
  Runs an accessible interview (no jargon), drafts docs/prd/{slug}.md from the template, hard-stops
  for approval, then drafts docs/adr/{date}-{slug}.md, registers it in the ADR index, and hard-stops
  again. Creates a feature branch if on main. Use when asked to "start a new feature", "kick off a
  feature", "begin the feature workflow", "write a PRD", "create an ADR", or to scope/plan a new
  capability before any code is written. Stops before implementation — it plans, it does not build.
---

# Start a new feature: PRD → ADR, planning only

A repeatable workflow for opening a new kora feature the way `AGENTS.md` mandates: **PRD first,
then ADR, then (separately) implementation**. This skill drives the two planning phases and stops.

> **The defining principle: plan the feature so the implementation is obvious, then stop.**
> The deliverable is two approved documents and a clean branch — *not* code. Implementation is a
> deliberately separate step the human starts afterwards, with the ADR as its contract. Do not
> write feature code, scaffold packages, or run build targets in this skill.

## Match the doc to the feature's weight

Before anything else, internalise the house style. **Read two or three recent docs of *comparable
weight* first** (`ls docs/prd/`, `ls docs/adr/`) and mirror their shape. The templates
(`docs/prd/template.md`, `docs/adr/template.md`) are a *menu, not a checklist*. Ceremony in kora
scales with architectural impact — a doc should be exactly as heavy as its feature, no heavier and
no lighter. There are two ends of the range; place the feature on it first, then pick a neighbour to
mirror.

**Lean end — an operational, UI, or reporting feature** (a card, a filter, a page, a column).
~100–200 lines.
- PRD (e.g. `docs/prd/budgets-page.md`, ~100 lines): Executive Summary → Problem Statement → Goals /
  Non-Goals → User Stories → Proposed Solution → Scope. **Drops** Requirements tables, Dependencies,
  Risks, Alternatives, Timeline, Appendix, Sign-offs.
- ADR (e.g. `docs/adr/2026-06-23-agent-token-breakdown-and-spend-card.md`, ~180 lines): Context →
  Options Considered → Decision → Implementation → Consequences. Context is plain problem-framing;
  Decision Drivers / Validation / Related Decisions dropped.

**Heavy end — a foundational or cross-cutting feature** (a new service, a new subsystem, a new
data/auth model). 600–1100+ lines. These earn extra machinery; reach for it when the feature
warrants it:
- **Big PRD** (e.g. `docs/prd/inference-gateway.md` 863 lines, `docs/prd/agent-control-plane.md` 662
  lines): many *numbered* user stories with acceptance criteria (`US-001 … US-013`); a Proposed
  Solution broken into subsections **named after the real components** (e.g. "Provider Connector
  Design", "Routing Engine", "Usage Record Schema"); explicit `Non-Goals (MVP)` / `Out of Scope
  (MVP)`; Functional Requirements **tiered P0/P1/P2** plus Non-Functional; multiple numbered
  Alternatives Considered; **Implementation Phases** with week targets; Open Questions; Appendix +
  Glossary.
- **Big ADR** (e.g. `docs/adr/2026-04-01-inference-gateway-architecture.md` 1166 lines,
  `docs/adr/2026-04-14-policy-gateway-and-authorisation-model.md` 773 lines): the signature move is
  to **decompose the decision into numbered sub-decisions** ("Decision 1: Authentication Approach" …
  "Decision 6: Policy Evaluation Point"), each with its own options/rationale. Then an **invented
  domain or `## Architecture` section** carries the real weight with concrete artifacts. Treat
  these as **contracts, not decoration** — the diagram is the component-connection contract, the Go
  interface is the API contract, the SQL is the data contract; their job is to remove ambiguity for
  whoever implements:
  - **ASCII diagrams** — request lifecycle / sequence flows / component topology.
  - **Go interface contracts** — the key types and method signatures.
  - **SQL schema** — `CREATE TABLE` for new tables.
  - **API request/response examples** and **config schema**.
  - A **domain model** section when the feature introduces new concepts (e.g. the policy ADR's "The
    Authorisation Model": Grant, Scope Vocabulary, Delegation Intersection invariant).
  - **Dense cross-links** to the ADRs it builds on, inline in the prose.
  - **Phased Implementation** (weeks), a **Future Work / deferred concerns** section, Validation
    (Success Metrics + Monitoring), Related Decisions.

**Then:** write the sections you have real signal for and cut the rest — but don't under-build a
foundational feature either. A spend card written like the inference gateway is as wrong as the
reverse. Where a section matters but you genuinely don't know yet, write `_TBD_` + an Open Question
rather than inventing content. **Size is set by technical machinery, not ceremony** — even the
biggest foundational docs don't invoke the three questions (see Phase 2); they get big by carrying
sub-decisions, contracts, schema, and diagrams.

## How to run it

Three phases, with a **hard stop for explicit human approval between each** — never start the ADR
until the PRD is approved, never declare done until the ADR is approved. Surface each artifact, ask
for approval in plain words, and wait. "Looks good / approved / continue" means go; anything else is
a revision request — revise and re-present.

**Interview in plain language.** Ask questions a brand-new kora engineer could answer — "what's
broken today?", "how do people work around it now?", "how would we know it worked?". Build context
progressively: each answer sharpens the next question. Don't interrogate the user with kora's
internal vocabulary.

---

## Phase 0 — Frame and branch

**Goal:** a one-line feature statement, awareness of prior art, and a clean branch.

1. Get the feature in one or two sentences. If the user only gave a title, ask: *"In a sentence or
   two — what are we building, and who's it for?"*
2. **Derive the slug** — kebab-case, feature-named, no date (e.g. `agent-webhook-triggers`). Reused
   for the PRD filename, the ADR filename, and the branch.
3. **Research prior art** before interviewing — it makes every later question sharper and prevents
   duplicate docs. In parallel:
   - `ls docs/prd/` and `ls docs/adr/`; **read the two or three most similar docs** to copy their
     shape and depth.
   - Grep `internal/` for the feature area and identify which feature package(s) and binary it
     likely touches (binary map is in `AGENTS.md`).
   - If a PRD or ADR already covers this, **stop and flag it** — the user may want to amend an
     existing doc, not open a near-duplicate.
4. **Branch.** If on `main`, create `feat/{slug}` and switch to it. If already on a feature branch,
   stay on it (confirm it's the right one). Never write planning docs onto `main`.

```bash
git rev-parse --abbrev-ref HEAD            # confirm current branch
git switch -c feat/{slug}                  # only if currently on main
```

Briefly report what you found (similar docs, affected packages, the branch) before moving on.

---

## Phase 1 — PRD (the *what* and *why*)

**Goal:** a `docs/prd/{slug}.md` the user approves. Product-level — problem, users, success, scope.
Keep implementation detail out; that's the ADR's job. Match the length of a similar recent PRD.

### Interview

Ask in plain language, a few at a time, adapting to answers. Cover enough to write honestly — don't
pad.

- **Problem & today's workaround** — *What's broken or missing right now? How do people deal with it
  today?* → Problem Statement, Pain Points.
- **Who feels it** — *Who hits this — an admin, a developer, an agent operator?* → User Stories
  (`As a … I want … so that …`).
- **Definition of done** — *When this ships, what can someone do that they can't today? How would we
  know it worked?* → Goals, Success Metrics.
- **Edges** — *What are we deliberately NOT doing in this first cut?* → Non-Goals, Out of Scope.
- **Anything else that constrains it** — only if it comes up: a deadline, a dependency, a hard
  security/compliance requirement. Don't fish for these; capture them if the user raises them.

For a **foundational** feature, push further: draw out *several* user stories (the big PRDs number
them `US-001…`), the distinct components the solution breaks into, and whether it ships in phases —
so the Proposed Solution, numbered User Stories, and Implementation Phases sections have real
substance. For a lean feature, a handful of plain questions is enough; don't inflate it.

### Draft

Write `docs/prd/{slug}.md` mirroring a recent similar PRD's section set (the lean spine above is the
default; add sections only when earned). Header block:

- **Status:** `Draft`
- **Created / Last Updated:** today — from `date +%F`, don't guess.
- **Owner:** the user (ask if unclear); **Stakeholders:** as discussed.

### Gate

Present the PRD (path + a tight summary of problem, goals, scope, any open questions). Ask plainly:
*"Does this capture it? Approve to move on to the ADR, or tell me what to change."* On approval,
flip Status to `In Review`, then commit:

```bash
git add docs/prd/{slug}.md && git commit -m "docs(prd): {feature title}"
```

**Do not start Phase 2 until approved.**

---

## Phase 2 — ADR (the *how*)

**Goal:** a complete `docs/adr/{YYYY-MM-DD}-{slug}.md`, **registered in the index**, that the user
approves. Default shape: Context → Options Considered → Decision → Implementation → Consequences.

### Research, then interview

First deepen the Phase 0 research into a real understanding of the affected code: the feature
package(s), the conventions they follow (repository pattern, typed platform clients, config loading,
controller-based routing), and any existing ADRs this builds on. **Propose** options grounded in how
kora already does things — don't ask the user to invent them cold.

Then interview on the decision:

- **Surface** — *Here's what I think this touches: {packages/binaries}. Right?* → Context.
- **Options** — present 2–3 realistic approaches **you** drafted from the codebase, with honest
  pros/cons, and ask which way they lean → Options Considered.
- **The call** — confirm the chosen option and capture *why it beats the others here* → Decision.
- **The shape of the build** — *roughly how does this get built — which layers change?* → a concrete
  Implementation section (this is where real ADRs spend their length).
- **The cost** — *What does this make harder, what breaks, what needs migrating?* → Consequences.

**For a foundational feature, go heavy here (see "Match the doc to the feature's weight").** If the
decision is really several decisions, decompose it into **numbered sub-decisions** and analyse each.
Then draft the concrete artifacts that make a big ADR load-bearing — an ASCII request-lifecycle /
topology diagram, the key Go interface contracts, `CREATE TABLE` schema, config/API examples, and a
domain-model section if new concepts appear — plus a phased Implementation and a Future Work
section. Don't invent these for a lean feature; do build them for a subsystem.

### Security & identity — only when the feature actually touches it

kora's spine is *Who is this agent? What can it access? How does it connect?* **Most features don't
touch any of these** — a spend card, a filter, a reporting page, a frontend change. Don't force the
framing into docs that don't need it; real kora ADRs for ordinary features never mention it.

**Only when** the feature genuinely involves identity (minting/delegating/attributing a principal),
access control (a new action that must be policy-checked before it runs), or outbound connectivity
(credentials/egress), add a short paragraph in Context addressing the relevant one(s) — does a
delegated agent stay within its delegator? is the new action default-deny and checked *before*
execution? are credentials kept away from the agent? The handful of ADRs that do this
(`2026-06-17-agent-triggers-and-webhook-ingestion.md`,
`2026-06-06-jit-credential-brokering-egress-gateway.md`) are good models. If in doubt whether it
applies, ask the user one plain question rather than manufacturing a section.

### Draft

Write `docs/adr/{YYYY-MM-DD}-{slug}.md` (filename date = today from `date +%F`). Header block:

- **Status:** `Proposed`
- **Date:** today.
- **Deciders:** the user + whoever else they name.
- **Technical Story:** link the PRD — `../prd/{slug}.md`.

### Register in the index — don't skip this

A new ADR is invisible until it's in `docs/adr/README.md`. Add it in **two** places:

1. **By subsystem** — under the best-fit heading (Foundations & Platform; Identity, Auth & Tokens;
   Policy & Authorization; Inference Gateway; Agents Control Plane & Execution; Service Mesh,
   Operator & Deployment; Frontend & Design System), as:
   `- [Title](YYYY-MM-DD-slug.md) — \`Proposed\` · YYYY-MM-DD`
2. **All ADRs (chronological)** table — insert a row in date order:
   `| YYYY-MM-DD | [Title](YYYY-MM-DD-slug.md) | Proposed | — |` (Former # is `—` for new ADRs).

### Gate

Present the ADR (path + chosen option and headline consequences) and confirm the index entries
landed. Ask plainly: *"Approve this ADR? On approval the planning phase is done and this is ready to
implement."* On approval, commit:

```bash
git add docs/adr/{YYYY-MM-DD}-{slug}.md docs/adr/README.md && git commit -m "docs(adr): {feature title}"
```

> Approving the ADR ratifies the *decision to build it this way* — it does **not** mean the code
> exists. Leave Status `Proposed` unless the user explicitly says to mark it `Accepted`.

---

## Phase 3 — Handoff

Planning is done. Summarise and stop:

- The branch (`feat/{slug}`) and the two committed docs with their paths.
- A short **implementation outline** — the packages/files likely to change and a rough task order,
  drawn from the ADR. A map for the next step, *not* code.
- Remind the user that implementation is the separate next step (their call), and that `make check`
  must pass before any PR.

Do not start coding, scaffold packages, or open a PR. If the user says "now build it", that's a new
piece of work that begins from the approved ADR.

---

## Reference

**Filename conventions** (verbatim from the repo):

| Artifact | Path | Naming |
|---|---|---|
| PRD | `docs/prd/{slug}.md` | feature-named, **no date** (e.g. `agent-webhook-triggers.md`) |
| ADR | `docs/adr/{YYYY-MM-DD}-{slug}.md` | decision date + slug, **no sequential number** |
| ADR index | `docs/adr/README.md` | entry in subsystem list **and** chronological table |
| Branch | — | `feat/{slug}` |

**Status lifecycles:** PRD `Draft → In Review → Approved → Implemented`. ADR
`Proposed → Accepted → Deprecated/Superseded`. This skill leaves PRD at `In Review` and ADR at
`Proposed` — humans ratify later.

**Gotchas / hard-won rules:**

- **Match the feature's weight — both ways.** Mirror a recent doc of *comparable size*. Trim every
  section you have no signal for on a lean feature; but don't under-build a subsystem either — a
  foundational ADR earns sub-decisions, contracts, schema, diagrams, and a phased plan. A spend card
  written like the inference gateway is as wrong as the reverse.
- **Size comes from technical machinery, not the three questions.** Even the 600–1100 line
  foundational docs (inference gateway, policy gateway, agent control plane) never invoke
  Who/What/How. That framing is a *separate, rare* device — use it only when the feature actually
  creates agent identity, gates access, or brokers egress (see Phase 2's security section), at any
  size.
- **Never write docs on `main`.** Phase 0 branches first. If you find yourself on `main` mid-flow,
  branch before the next write.
- **Get the date from `date +%F`** — never guess it for filenames or headers.
- **Plan, don't build.** The most common failure is sliding from ADR into implementation. Stop at
  the gate; code is a separate task.
- **Check for an existing doc first.** If prior art already covers the feature, amending it usually
  beats opening a near-duplicate — surface that choice to the user.
- **Both index entries.** An ADR in only one of the two places (or neither) is the easiest thing to
  forget and the whole point of the index.
