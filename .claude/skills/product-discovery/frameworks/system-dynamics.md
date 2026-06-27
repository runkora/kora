# Lens: System Dynamics & leverage points

**What it is.** See the platform as **stocks, flows, and feedback loops**, not a list of features.
Donella Meadows' insight: the highest-leverage interventions are rarely the obvious ones (adding
a feature) — they're changes to the loops, the information flows, the rules, and the goals of the
system. This lens finds pillars that *compound*.

## Procedure

1. **Name the stocks** — things that accumulate. For Orbit: deployed governed agents, trust in
   the platform, audit history, policies defined, tenants, credential incidents (a stock you want
   to drain).
2. **Name the flows** that fill/drain them, and the **feedback loops:**
   - **Reinforcing (R)** loops compound (good or bad): more governed agents → more audit data →
     better policy suggestions → easier to govern the next agent → more governed agents.
   - **Balancing (B)** loops resist change / seek a goal: friction to onboard an agent balances
     against adoption.
3. **Find leverage points** (Meadows, high→low leverage): the goal of the system > the rules >
   information flows > feedback loop strength > buffers/stocks > parameters. Prefer pillars that
   act high on this list. "Make the policy decision visible to the agent in real time" (an
   *information flow*) outranks "add a setting" (a *parameter*).
4. **Trace second-order effects:** what does the pillar incentivize? What loop does it strengthen
   or break? Where could a reinforcing loop run away (e.g. auto-tightening scope → agents
   constantly blocked → humans disable governance)?
5. Output: the key loop(s) the pillar creates/changes, the leverage level, and the
   compounding-adoption story in dual-user terms.

## Orbit micro-example

Reinforcing loop to design for: every governed agent emits trace/audit → the event spine
aggregates → Orbit proposes tighter, safer policies from real behaviour → onboarding the next
agent is faster and safer → more agents onboarded. The **event spine** pillar isn't a feature;
it's the information flow that powers the loop — high leverage. Watch the runaway: auto-proposed
policy must stay suggest-not-enforce or the balancing loop (human trust) breaks.

## Failure modes

- **Listing features, not loops.** If your output has no loop in it, you used a different lens.
- **Low-leverage fixation.** Parameters/settings feel concrete but are the weakest interventions.
- **Ignoring the dark loops.** Every reinforcing loop can run away — name the balancing force.
