# App-level shared components

This document is the authoritative index of Tier 2 app-shared components: components that are Orbit-domain-aware, used by two or more distinct dashboard features, but not generic enough for Terra. All components import from `@orbit/components`.

Before writing any UI boilerplate, check this list. If a component already covers the need, use it. If it almost covers the need, extend it via props rather than creating a new file.

---

## EmptyState

**Import:** `import { EmptyState } from '@orbit/components'`

**When to use:** Any section that can have zero items and needs a centred placeholder with an icon, title, description, and an optional CTA button.

**When not to use:** Full-page error states (use `Alert variant="error"` from `@orbit/terra`); loading skeletons (use Terra `Skeleton`).

```tsx
interface EmptyStateProps {
  icon: IconName;
  title: string;
  description: string;
  action?: React.ReactNode;
}
```

```tsx
<EmptyState
  icon="key"
  title="No API keys yet"
  description="Create a key to start making requests to the inference gateway."
  action={<Button size="sm">Create your first key</Button>}
/>
```

---

## StatusBadge

**Import:** `import { StatusBadge } from '@orbit/components'`

**When to use:** Any place a resource's health/lifecycle state needs to be shown as a labelled badge with a status dot.

```tsx
type OrbitStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'unknown';

interface StatusBadgeProps {
  status: OrbitStatus;
  label?: string; // override auto-capitalised default
}
```

```tsx
<StatusBadge status="healthy" />
<StatusBadge status="degraded" label="Bundle stale" />
```

---

## ConfirmDialog

**Import:** `import { ConfirmDialog } from '@orbit/components'`

**When to use:** Any destructive action that requires user confirmation: delete, revoke, reset. Composes `Modal` from Terra internally.

**When not to use:** Non-destructive confirmations where a simple button pair suffices; use a standard inline form instead.

```tsx
interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  confirming?: boolean;
  error?: string | null;
  children?: React.ReactNode; // extra body content below description
}
```

```tsx
<ConfirmDialog
  open={!!deleteTarget}
  title="Delete Role"
  description={`Remove "${deleteTarget?.role}" and all its scope mappings? This cannot be undone.`}
  confirmLabel="Delete"
  confirming={isDeleting}
  error={deleteError}
  onConfirm={() => void handleDelete(deleteTarget.role)}
  onClose={() => setDeleteTarget(null)}
/>
```

---

## MetricCard

**Import:** `import { MetricCard } from '@orbit/components'`

**When to use:** Any KPI/metric display in an overview or dashboard: icon + label + value + optional delta + optional breakdown rows + optional "View all" link.

```tsx
interface MetricCardProps {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  delta?: { text: string; positive: boolean; positiveIsBad?: boolean };
  breakdown?: Array<{ label: string; value: string | number }>;
  href?: string; // renders "View all →" footer link
}
```

```tsx
// Metric with delta
<MetricCard
  icon={<Icon name="activity" />}
  label="Total Requests"
  value={fmtNumCompact(requests.total)}
  delta={{ text: '▲ 3.2% vs last 7 days', positive: true }}
/>

// Metric with breakdown and footer link (policy-style)
<MetricCard
  icon={<Icon name="shield-check" />}
  label="Grants"
  value={stats.grants.total}
  href={APP_ROUTES.policy.grants}
  breakdown={[{ label: 'Human', value: 12 }, { label: 'Agent', value: 34 }]}
/>
```

---

## EventFeedItem

**Import:** `import { EventFeedItem } from '@orbit/components'`

**When to use:** A single row in a `<ul className="divide-y">` activity feed or event list. Status dot + primary label + optional meta + message + timestamp.

```tsx
interface EventFeedItemProps {
  status: OrbitStatus;
  label: string; // primary identifier, monospace (e.g. event type)
  meta?: string; // supporting identifier (e.g. agent name)
  message: string;
  timestamp: string;
}
```

```tsx
<ul className="divide-y divide-border-subtle">
  {events.map((evt) => (
    <EventFeedItem
      key={evt.id}
      status={evt.severity}
      label={evt.type}
      meta={evt.agent}
      message={evt.message}
      timestamp={evt.ts}
    />
  ))}
</ul>
```

---

## ScopeChipList, ScopeChip and scopeColumnFilter

**Import:** `import { ScopeChip, ScopeChipList, scopeColumnFilter } from '@orbit/components'`

**When to use:** Any place an Orbit scope string is shown. `ScopeChip` is one highlighted scope (truncating, with the full scope as its tooltip). `ScopeChipList` shows a list of them, collapsing the surplus into a "+N more" button that opens an overlay with every scope. `scopeColumnFilter` builds the searchable multi-select column filter for a scope column.

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

## TruncatingBadge

**Import:** `import { TruncatingBadge } from '@orbit/components'`

**When to use:** A badge whose label can grow (an adapter, an agent name) inside a table cell. It shrinks to the width the container allows, ends in an ellipsis and shows the full label as its tooltip.

```tsx
<Stack direction="horizontal" minWidth="none">
  <TruncatingBadge label={name} variant="outline" textColor="foreground" />
</Stack>
```

Place it inside a row or wrap `Stack`, not directly in a block cell, or it stretches to the cell's full width.

---

## BadgeList and ClampedList

**Import:** `import { BadgeList, ClampedList } from '@orbit/components'`

**When to use:** `BadgeList` shows plain outline badges (capabilities, dialects, modalities) as many as fit in `maxRows` rows, with the rest behind a "+N more" overlay. `ClampedList` is the layer underneath, shared with `ScopeChipList`: give it `items`, `getKey`, `renderItem` and a `noun` for the trigger's accessible name when the items are something else.

```tsx
<BadgeList values={row.capabilities} noun="capabilities" maxRows={2} />
```

---

## PrincipalBadge

**Import:** `import { PrincipalBadge } from '@orbit/components'`

**When to use:** Anywhere a principal's type (agent, human, PAT, service) is shown. One colour per type, so the type reads the same on every page. In a table give it its own column rather than placing it beside the name.

```tsx
{ id: 'type', header: 'Type', cell: (g) => <PrincipalBadge type={g.principal_type} /> }
```

---

## useClampedRows

**Import:** `import { useClampedRows } from '@orbit/components'`

**When to use:** A wrapping list in a cell that should show as many items as fit in a fixed number of rows, then a "+N more" trigger. It is a hook, so the caller owns the markup: render every item, then a trigger element marked `data-clamp-trigger` with no children. The hook hides what does not fit and writes the trigger's text. See `ScopeChipList` and the connections `UsedByList` for both usages.

---

## ThemeToggle

**Import:** `import { ThemeToggle } from '@orbit/components'`

**When to use:** The user-facing control that switches between the light and dark
themes (typically placed in the dashboard header).

**How it works:** Calls `setTheme(...)` from `next-themes`, which flips the
`data-theme` attribute on `<html>`. Terra's design tokens re-colour the entire UI
from that attribute, so no other wiring is needed. It renders both a sun (switch
to dark) and a moon (switch to light) `Button`; `globals.css` shows the one that
matches the active theme so the control is correct at first paint. Takes no props.

For how theming works end-to-end and how to author theme-aware components (use
tokens, never hardcoded colours), see
[Theming](./terra-design-system.md#theming-light--dark) in the Terra reference.

```tsx
<ThemeToggle />
```
