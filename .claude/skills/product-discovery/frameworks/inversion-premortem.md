# Lens: Inversion & Pre-mortem (adversarial)

**What it is.** Instead of asking "how do we succeed?", ask "how do we *guarantee failure*?" and
then avoid those things. Inversion sharpens divergence (in Phase 2) *and* is the primary tool of
the STRESS phase (Phase 4), where it red-teams surviving pillars.

## Procedure (two uses)

### As an ideation lens (Phase 2)
1. Invert the goal: "How would we make Orbit's agents *maximally ungovernable / unsafe / painful
   to operate*?" List the mechanisms.
2. Each failure mechanism, negated, is a candidate requirement or pillar. ("Agents accumulate
   standing access" → pillar: access that decays / is per-call.)

### As a stress test (Phase 4) — run on each surviving pillar
1. **Pre-mortem.** Assume it's 18 months later and this pillar *failed*. Write the obituary: the
   top 3 concrete reasons it failed (adoption, trust, complexity, it weakened something).
2. **Security-guarantees regression check.** A pillar may open net-new ground beyond the three
   questions — that's fine. What it must **not** do is erode the guarantees that make Orbit
   credible: verifiable identity, **pre-execution** policy, **no credentials to agents**, and
   full **traceability** to the initiating human. Be specific — a credential-convenience feature
   that lets agents cache tokens breaks "no creds to agents"; a "fast path" that skips the PEP
   breaks pre-execution policy. If it erodes a guarantee, redesign or drop. (Not advancing a
   particular one of Who/What/How is not a failure; *weakening a guarantee* is.)
3. **Second-order / incentive check.** What does it make humans or agents do that you didn't
   intend? (A noisy policy-deny UX → humans grant broad scopes to stop the noise → governance
   theater.)
4. **Adversary view.** How would a malicious agent or insider abuse it?
5. Output per pillar: pass/redesign/drop + the mitigations that move it to pass.

## Orbit micro-example

Pre-mortem on a "tool gateway" pillar: failed because every new tool needed hand-written policy,
so teams routed around it. Mitigation: default-deny with one-click scoped templates derived from
observed calls (ties to the event-spine loop). Three-questions check: strengthens What, must not
let the agent self-approve tools (would weaken What) → approval stays human/delegated.

## Failure modes

- **Soft pre-mortems.** "It might not get adoption" is useless; name the *mechanism* of failure.
- **Skipping the regression check.** The most important Orbit-specific gate — a pillar that
  advances one question while quietly weakening another is a net loss.
- **Only red-teaming the happy path.** Include the malicious-agent and lazy-human paths.
