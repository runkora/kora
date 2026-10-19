# App-level shared components

This document is the authoritative index of Tier 2 app-shared components: components that are kora-domain-aware, used by two or more distinct dashboard features, but not generic enough for Hades. All components import from `@runkora/components`.

Before writing any UI boilerplate, check this list. If a component already covers the need, use it. If it almost covers the need, extend it via props rather than creating a new file.

---

## StatusBadge

**Import:** `import { StatusBadge } from '@runkora/components'`

**When to use:** Any place a resource's health/lifecycle state needs to be shown as a labelled badge with a status dot.

```tsx
type KoraStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'unknown';

interface StatusBadgeProps {
  status: KoraStatus;
  label?: string; // override auto-capitalised default
}
```

```tsx
<StatusBadge status="healthy" />
<StatusBadge status="degraded" label="Bundle stale" />
```

---

## ScopeChipList, ScopeChip and scopeColumnFilter

**Import:** `import { ScopeChip, ScopeChipList, scopeColumnFilter } from '@runkora/components'`

**When to use:** Any place a kora scope string is shown. `ScopeChip` is one highlighted scope (truncating, with the full scope as its tooltip). `ScopeChipList` shows a list of them, collapsing the surplus into a "+N more" button that opens an overlay with every scope. `scopeColumnFilter` builds the searchable multi-select column filter for a scope column.

```tsx
interface ScopeChipListProps {
  scopes: readonly string[];
  max?: number; // chips shown before "+N more"; default 3
  maxRows?: number; // instead of a count: as many as fit in this many rows
}

// In a DataTable column:
{ id: 'scopes', header: 'Scopes', wrap: true, filter: scopeColumnFilter(rows, (r) => r.scopes),
  cell: (r) => <ScopeChipList scopes={r.scopes} maxRows={2} /> }
```

See [table-layout.md](../golden-paths/table-layout.md) for the sizing rules around these.

---

## ClampedList

**Import:** `import { ClampedList } from '@runkora/components'`

**When to use:** A list in a cell that shows as many items as fit in `maxRows` rows, with the rest behind a "+N more" overlay. It is the layer under `ScopeChipList`: give it `items`, `getKey`, `renderItem` and a `noun` for the trigger's accessible name. Anything that follows the table layout golden path for a collection column can render through it.

```tsx
<ClampedList
  items={row.connectors}
  getKey={(c) => c.connection}
  renderItem={(c) => (
    <TruncatingBadge label={c.connection} variant="outline" textColor="foreground" />
  )}
  noun="connectors"
  maxRows={2}
/>
```

---

## PrincipalBadge

**Import:** `import { PrincipalBadge } from '@runkora/components'`

**When to use:** Anywhere a principal's type (agent, human, PAT, service) is shown. One colour per type, so the type reads the same on every page. In a table give it its own column rather than placing it beside the name.

```tsx
{ id: 'type', header: 'Type', cell: (g) => <PrincipalBadge type={g.principal_type} /> }
```

---

## useClampedRows

**Import:** `import { useClampedRows } from '@runkora/components'`

**When to use:** A wrapping list in a cell that should show as many items as fit in a fixed number of rows, then a "+N more" trigger. It is a hook, so the caller owns the markup: render every item, then a trigger element marked `data-clamp-trigger` with no children. The hook hides what does not fit and writes the trigger's text. See `ScopeChipList` and the connections `UsedByList` for both usages.

## RUN_STATE_DISPLAY

`RUN_STATE_DISPLAY` maps a run state to the status dot and label every run surface shows. A `pending` run reads Queued, `starting` reads Starting and `active` reads Running.

```tsx
import { RUN_STATE_DISPLAY } from '@runkora/components';

const dot = RUN_STATE_DISPLAY[run.state];
<StatusDot status={dot.status} role="img" aria-label={dot.label} />;
```
