# Lens: Jobs To Be Done (JTBD) — dual-user

**What it is.** People (and here, agents) don't buy products; they "hire" them to make progress
on a *job* in a given situation. Anchor pillars to the job, not the feature — features are
hireable means, jobs are stable ends. For Orbit, the job has **two hirers** and you must model
both.

## Procedure

1. **Write the job statement** in the canonical form, situation-anchored:
   `When [situation], I want to [motivation], so I can [expected outcome].`
2. **Do it twice — once per user population:**
   - **Human hirer** (admin / security / developer): e.g. *When I'm putting an agent into
     production, I want to guarantee it can't exceed its remit, so I can be accountable for what
     it does.*
   - **Agent hirer** (the autonomous consumer): e.g. *When I need to call an upstream tool, I
     want valid scoped access without handling secrets, so I can complete my task without
     failing on auth or over-reaching.*
3. For each job, list **functional**, **emotional**, and **social** dimensions; then the **pains**
   (what's hard/slow/risky today) and **gains** (what "great" looks like).
4. Identify **underserved or overserved** outcomes — that gap is where a pillar earns its place.
5. Tie the job to Who/What/How and to the dual-user value framing the PRD will need.

## Orbit micro-example

Agent job: *When I run in a customer's prod and must touch their database, I want just-enough,
just-in-time access I didn't have to ask a human for, so I don't stall and don't become a
standing credential risk.* Partly served today — the credential broker pattern exists (GitHub
App + passthrough adapters, egress control) but is thin on breadth of upstreams, so the job is
underserved beyond a few connectors. Human counterpart:
*…so I can prove, after the fact, exactly which human's authority every DB touch traced back
to.* One pillar, two satisfied hirers → strong candidate.

## Failure modes

- **Feature-as-job.** "Users want a credential broker" is not a job; "complete a task without
  handling secrets" is.
- **Forgetting the agent hirer.** The most common miss in Orbit discovery. If you only wrote a
  human job, you're not done.
- **Solution-laden jobs.** Keep the job statement free of implementation; the *how* comes later
  and not in this skill at all (PRD stays conceptual).
