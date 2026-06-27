# Orbit Opportunity Radar

A living, **append-only** record of product-discovery scans. Each `product-discovery` run (see
`.claude/skills/product-discovery/`) appends a dated section with its ranked opportunity slate.

**Why this is durable (not scratch):** the radar is reusable IP — the directions a scan
*deferred* are future discovery seeds, and the rationale + sources behind a pick are worth
keeping. Raw per-lens ideation stays ephemeral in `.scratch/`; this file is the signal.

**How to use it:**
- New runs **append** a section; don't rewrite history.
- Each direction records a **Horizon** (H1 incremental · H2 adjacent · H3 transformative) and its
  **novelty-weighted score** — every slate should carry at least one H2/H3 "big swing".
- Before a fresh scan, skim past **Deferred** directions — they may now be ripe.
- When a direction graduates, update its **Status** in place (→ link the pillar map / PRD).

**Status legend:** `Selected` (carried into the pipeline this run) · `Deferred` (parked as a
future seed) · `Promoted` (became a pillar map / PRD — link it) · `Dropped` (with reason).

---

## 2026-06-27 — Autonomous scan (no seed)

First discovery run. Scanned Orbit's own state (built control plane + the two empty stubs
`internal/toolgateway/` and `internal/events/`; forming pillars Playground & Context Lake) against
the June-2026 agentic-platform / agent-governance market. _Shakedown run — the draft pillar /
PRD / dossier artifacts were kept in `.scratch/` and not committed._

| # | Direction | Status | Horizon | Novelty | Pillar (extends / new) | Why-now (one line) |
|---|---|---|---|---|---|---|
| 1 | **Tamper-evident Audit & Governance event spine** → *Verifiable Provenance* | Selected | H2 adjacent | 4/5 | Audit & Governance (+ builds `events` spine) | EU AI Act Art. 12 full application 2026-08-02; tamper-evidence is the named gap; Orbit uniquely holds the identity+policy context that makes a record meaningful |
| 2 | **Recursive A2A delegation & scope-narrowing** → *Verifiable Delegation Chains* | Selected | **H3 transformative** (the big swing) | 5/5 | Identity & Policy (Who/What); opens new **Workflow Governance** sub-pillar | Shift human→agent ⇒ agent→agent; "recursive delegation accountability" a top research gap; maps exactly to Orbit's "agent never exceeds delegator" guarantee — Orbit as the enforcement runtime AIP/OAuth define on paper but nobody ships |
| 3 | **Governed MCP / tool gateway** | Selected | H1→H2 | 2/5 | What can it access? (fills `toolgateway` stub) | MCP is the de-facto tool protocol; crowded gateway field; Orbit's in-pod edge (Envoy/OPA) + no-creds-to-agents on one governance plane is the differentiator |
| 4 | **Governed Context Lake (policy-scoped agent memory)** | Deferred | H3 transformative | 4/5 | Context Lake (forming) | Strong agent-as-user angle but speculative; crowded un-governed-memory field (Mem0, Zep) — parked as a future seed |

**Big-swing check:** the slate carries one H3 transformative pick (#2 Verifiable Delegation Chains —
opens a new sub-pillar and a category nobody ships). #1 is adjacent-but-novel; #3 is the
incremental table-stakes fill. Deferred #4 is the next H3 seed.

**Value read** (human / agent / blueprint):
- #1 — compliance & defensibility / verifiable replayable self-record / flagship "verify us without trusting us" regulated-client demo
- #2 — multi-agent accountability in one provable object / secretless attenuable sub-delegation / reusable NHI-governance reference control vs static-cred identity vendors
- #3 — per-tool allow-deny + audit on the same plane as models/runs / safe tool use without holding creds / anti-API-gateway story

**Market-scan sources:** EU AI Act Art. 12 audit-log mandate · IETF `draft-sharif-agent-audit-trail`
· kiteworks tamper-evident/SIEM · agentsystems "half the audit trail" · arXiv 2603.24775 (AIP:
Verifiable Delegation across MCP+A2A) · SC Media "M2M to A2A" · Strata/Kong/MintMCP/Integrate.io MCP
gateway landscape · Mem0 / Atlan agent-memory state-of-2026. (Full list in the run's scratch slate.)

> Next scan: re-check **#4 Context Lake** (may ripen as memory-governance matures), and watch
> Playground (forming, under-built).
