# Roadmap Pillar: [Pillar Name]

**Status:** [Candidate | Validated | Active]
**Created:** YYYY-MM-DD · **Owner:** [Name/Role]
**Discovery source:** [research dossier path + ideation run]

> This is the **strategy / product-fit** artifact — where this pillar sits in the platform
> long-term and what features ladder up to it. It is *not* a spec (that's the PRD) and *not*
> architecture (that's a later ADR). Keep it at the altitude of "why this, why now, where it goes".

---

## One-line thesis

[The pillar in a sentence — a durable value theme, not a feature.]

## The job (dual user)

- **Human hirer:** When [situation], the operator/admin/dev wants to [motivation] so they can [outcome].
- **Agent hirer:** When [situation], the agent consuming Orbit wants to [motivation] so it can [outcome].

## Platform fit

- **Product pillar:** [extends `Agents` / `Identity & Policy` / `Inference & Model Gateway` /
  `Audit & Governance` / `Playground` / `Context Lake` / `Infrastructure & Deployment` /
  `Enablement` — or **opens a new pillar:** `<name>`]. Taxonomy in `references/orbit-context.md`.
- **Builds on:** [existing capability — e.g. the credential broker pattern / egress control /
  inference gateway / PEP. Verify current state against the code, not stale docs.]
- **Relationship to the empty stubs:** [MCP/tool gateway · event spine — if relevant.]
- **Relationship to the three questions:** [deepens one — say which — *or* "net-new ground
  beyond Who/What/How; coherent with the thesis". Don't force a fit.]
- **Security-guarantees check:** does NOT erode verifiable identity / pre-execution policy /
  no-creds-to-agents / traceability. [confirmed in STRESS phase]
- **What it is NOT:** [explicit guardrail vs. "Orbit is not a framework/router/wrapper"].

## Differentiation (from Blue Ocean lens)

- **Raise/Create factors** that make this "why us":
- **What we deliberately don't do here:**

## The compounding story (from system-dynamics lens)

- **Loop it creates/strengthens:** [reinforcing loop → adoption/trust/safety compounds]
- **Balancing force to respect:** [what stops the loop running away]

## Emergent signal

[If two+ independent lenses converged on this pillar from different angles, say which — that
cross-lens agreement is the strongest evidence it's real.]

## Features that ladder up (now / next / later)

| Horizon | Feature | Primary value (human / agent) | → PRD |
|---|---|---|---|
| Now | | | [`docs/prd/...`] |
| Next | | | |
| Later | | | |

## Dependencies & sequencing

- Depends on: [other pillars / built capabilities]
- Unblocks:

## Stress-test summary (from Phase 4)

- **Top failure mode (pre-mortem):** … → **mitigation:** …
- **Three-questions regression:** pass / redesigned because …
- **Second-order effects watched:** …

## Open questions

- …
