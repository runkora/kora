# kora Frontend Docs

Reference material for working in the kora frontend. Start with the
[frontend README](../README.md) for setup and the dev workflow, and
[`AGENTS.md` → **Frontend Development**](../../AGENTS.md) for the Hades rules and
the component placement decision tree.

## Reference

- [hades-design-system.md](reference/hades-design-system.md): Hades component usage, props, examples, and the verification checklist. Authoritative for how to write product JSX.
- [component-placement.md](reference/component-placement.md): the three-tier component model (Hades / `@orbit/components` / feature-scoped), the full Hades inventory, and `@tier` annotation rules.
- [app-components.md](reference/app-components.md), index of Tier 2 app-shared components (`@orbit/components`): StatusBadge.

## Golden paths

The [Marketplace frontend deep dive](../../docs/architecture/marketplace.md) maps the browse hook/context and dedicated server-rendered Skill detail route to their files and request boundaries.

- [golden-paths/frontend-state-pattern.md](golden-paths/frontend-state-pattern.md), the end-to-end pattern for data-driven pages: server service module → BFF route → client service → custom hook → Context → RSC page.
- [golden-paths/table-layout.md](golden-paths/table-layout.md), how to size, truncate and clamp a table so nothing collapses, overlaps or overflows its card.
