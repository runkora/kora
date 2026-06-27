# Lens: Systematic Inventive Thinking (SIT)

**What it is.** Innovation by manipulating the components of an existing system rather than
brainstorming from a blank page. Descended from TRIZ. Pairs with the **Closed World** principle
(see that card) — solutions use resources already present — and **Function Follows Form**: start
from a hypothetical altered form, *then* discover the benefit (the reverse of "find a need, then
solve it"). This reversal is what makes it produce non-obvious ideas.

## Procedure

1. **List the components** of the system in scope (objects, actors, attributes, steps). For
   Orbit a "component" might be: the agent identity, the policy check, the credential injection,
   the audit event, the operator, a tenant, the sidecar, a tool call.
2. Apply each of the **five techniques** to those components. For each, first describe the
   altered form, *then* ask "what would that be good for?":
   - **Subtraction** — remove a component that seems essential. (Remove the agent's ability to
     hold *any* credential, ever. → forces just-in-time injection as the only path.)
   - **Division** — split a component and rearrange in time/space. (Split "what an agent can
     access" into deploy-time grant vs. per-call elevation.)
   - **Multiplication** — copy a component and change the copy. (A second, shadow policy
     evaluation that runs in "would-have-denied" mode for safe policy rollout.)
   - **Task Unification** — make an existing component do an extra job. (The audit event stream
     *also* becomes the training signal for policy suggestions.)
   - **Attribute Dependency** — make two attributes vary together that didn't before. (Access
     scope shrinks automatically as an agent's anomaly score rises.)
3. For each promising altered form, write the **benefit in dual-user terms** (human + agent)
   and tag which of Who/What/How it touches.
4. Keep the weird ones. SIT's value is the ideas that feel backwards at first.

## Orbit micro-example

*Subtraction* on the credential broker: remove the broker's credential *store* entirely. The
altered form: a broker that holds nothing and brokers a fresh upstream-issued token per call.
Benefit (human): no secret store to breach or rotate. Benefit (agent): never blocked on stale
creds. Advances **How does it connect?**, doesn't weaken Who/What.

## Failure modes

- **Naming, not applying.** Don't list "we used Subtraction"; show the removed component and the
  discovered benefit.
- **Need-first thinking.** SIT is form-first. If you started from a need, you used a different lens.
- **Discarding the absurd too early.** The point is to sit with the altered form long enough to
  find its use. That's where emergence lives.
