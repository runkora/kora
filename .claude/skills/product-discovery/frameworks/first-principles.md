# Lens: First Principles

**What it is.** Strip a problem down to the things that are *necessarily* true — physics,
constraints, invariants — and rebuild upward, ignoring how the category does it today. Breaks
inherited assumptions ("agents authenticate like services do") that quietly bound everyone else's
roadmap.

## Procedure

1. **State the conventional approach** and the assumptions baked into it. ("Agents get a
   long-lived API key like a microservice." "Access is granted per-service.")
2. **Challenge each assumption** with "why must this be true?" until you hit bedrock — something
   that genuinely cannot be otherwise. Bedrock for Orbit tends to be: *an action must trace to a
   human authority; access must be checked before execution; agents are non-deterministic and
   can't be trusted to self-limit.*
3. **Rebuild from bedrock**, ignoring convention. Given only the invariants, what's the simplest
   thing that satisfies them? Often it differs sharply from the conventional approach.
4. **Check the rebuild against reality** (the dossier, the Closed-World resources) — first
   principles generates the *shape*; feasibility keeps it honest.
5. Output: assumptions broken, the bedrock invariants, and the rebuilt approach as dual-user value.

## Orbit micro-example

Convention: "an agent needs credentials to call upstream systems." Challenge: *why must the agent
hold the credential at all?* Bedrock: the upstream must receive *a* valid credential bound to an
authorized identity at call time — nothing requires the *agent* to be the holder. Rebuild:
credentials never reach the agent; the edge injects them per call. (This is, not coincidentally,
Orbit's actual thesis — first-principles reliably regenerates it, which is a good sign the thesis
is sound, and the lens extends the existing broker pattern — built today with GitHub App +
passthrough adapters — to arbitrary upstreams.)

## Failure modes

- **Stopping above bedrock.** "Agents need scoped tokens" is still convention; push to *why*.
- **Rebuilding in a vacuum.** Pair with Closed-World/dossier so the rebuilt shape is buildable.
- **Confusing first-principles with contrarianism.** The goal is correct invariants, not just
  rejecting the status quo for its own sake.
