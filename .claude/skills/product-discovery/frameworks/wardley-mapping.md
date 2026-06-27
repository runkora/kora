# Lens: Wardley Mapping

**What it is.** A strategy map: a value chain (user need at top → the components it depends on
below) plotted against **evolution** (left→right: Genesis → Custom-built → Product/rental →
Commodity/utility). It reveals *where to invest*, what's commoditizing (and so shouldn't be a
differentiator), and which components are still genesis (where new pillars are born). The single
best lens for **roadmap shape**.

## Procedure

1. **Anchor on a user need** — and remember the dual user. Put the human need and the agent need
   at the top of the chain.
2. **Chain the dependencies** downward: what the need requires, what those require, down to the
   commodity substrate. For Orbit: *governed prod agent* → *identity* + *policy* + *credential
   access* + *tool access* → SPIFFE / OPA / Envoy / k8s (commodities).
3. **Place each component on the evolution axis.** Rough is fine. Identity-via-SPIFFE is
   product/commodity; a *governed tool gateway* is genesis/custom; the *event spine* is custom.
4. **Read the map for moves:**
   - Genesis/custom components that sit close to the user need = **pillar candidates** (build,
     differentiate).
   - Commodity components = **don't differentiate here**, consume them (this is the "Orbit is
     NOT a model router" discipline in map form).
   - Watch evolution flow: anything drifting toward commodity shouldn't anchor the roadmap.
5. Note **inertia** (what the org/market is attached to) and **climate** (where evolution is
   heading) to sequence now/next/later.
6. Output the map as text (chain + each component's evolution stage + the recommended moves).

## Orbit micro-example

Need (agent): *safe outbound tool calls*. Chain → tool gateway → policy decision → identity →
SPIFFE. SPIFFE/OPA are commodity (consume), the **tool gateway is genesis** and sits high in the
chain → strong pillar to build now. The inference gateway, by contrast, is sliding toward
product/commodity — keep it as enabler, don't pitch it as the differentiator.

## Failure modes

- **Mapping without the user need at the top.** Then it's an architecture diagram, not a Wardley map.
- **Over-precision on evolution placement.** Relative position matters, not exactness.
- **Recommending investment in commodities** — the classic strategic error the map exists to prevent.
