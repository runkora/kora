# App-level shared components

This document is the authoritative index of Tier 2 app-shared components — components that are Orbit-domain-aware, used by two or more distinct dashboard features, but not generic enough for Terra. All components import from `@orbit/components`.

Before writing any UI boilerplate, check this list. If a component already covers the need, use it. If it almost covers the need, extend it via props rather than creating a new file.

---

## EmptyState

**Import:** `import { EmptyState } from '@orbit/components'`

**When to use:** Any section that can have zero items and needs a centred placeholder with an icon, title, description, and an optional CTA button.

**When not to use:** Full-page error states (use `ErrorBanner`); loading skeletons (use Terra `Skeleton`).

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

## ErrorBanner

**Import:** `import { ErrorBanner } from '@orbit/components'`

**When to use:** Inline error message when a server action or data fetch fails. Renders an alert icon + error text inside a subtle red border box.

**When not to use:** Validation errors on individual form fields (use `FormField` error state from `@orbit/terra`); full-page unavailability (use a page-level error boundary).

```tsx
interface ErrorBannerProps {
  message: string;
}
```

```tsx
<ErrorBanner message="Unable to load policy stats. The policy service may be unavailable." />
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

> **`ErrorBanner` has been removed.** Use `<Alert variant="error">` from `@orbit/terra` with `<Icon name="triangle-alert" color="status-error" />` and `<Text size="caption" color="status-error">` as children. The `Alert error` variant now carries the same visual style (`rounded-lg`, `px-4 py-3`, `bg-status-error/5`).

---

## ConfirmDialog

**Import:** `import { ConfirmDialog } from '@orbit/components'`

**When to use:** Any destructive action that requires user confirmation — delete, revoke, reset. Composes `Modal` from Terra internally.

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

**When to use:** Any KPI/metric display in an overview or dashboard — icon + label + value + optional delta + optional breakdown rows + optional "View all" link.

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
  label: string;    // primary identifier, monospace (e.g. event type)
  meta?: string;    // supporting identifier (e.g. agent name)
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
