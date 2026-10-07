# kora Frontend Docs

Reference material for working in the kora frontend. Start with the
[frontend README](../README.md) for setup and the dev workflow, and
[`AGENTS.md` → **Frontend Development**](../../AGENTS.md) for the Terra rules and
the component placement decision tree.

## Reference

- [terra-design-system.md](reference/terra-design-system.md): Terra component usage, props, examples, and the verification checklist. Authoritative for how to write product JSX.
- [component-placement.md](reference/component-placement.md): the three-tier component model (Terra / `@orbit/components` / feature-scoped), the full Terra inventory, and `@tier` annotation rules.
- [app-components.md](reference/app-components.md), index of Tier 2 app-shared components (`@orbit/components`): StatusBadge.

## Golden paths

- [golden-paths/frontend-state-pattern.md](golden-paths/frontend-state-pattern.md), the end-to-end pattern for data-driven pages: server service module → BFF route → client service → custom hook → Context → RSC page.
- [golden-paths/table-layout.md](golden-paths/table-layout.md), how to size, truncate and clamp a table so nothing collapses, overlaps or overflows its card.
