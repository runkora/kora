/** Zero-state placeholder: icon + title + description + optional action. */
export { EmptyState } from './empty-state';
export type { EmptyStateProps } from './empty-state';

/** Maps an OrbitStatus value to a labelled Badge with a status dot. */
export { StatusBadge } from './status-badge';
export type { StatusBadgeProps, OrbitStatus } from './status-badge';

/** Destructive-action confirmation modal composing Terra Modal. */
export { ConfirmDialog } from './confirm-dialog';
export type { ConfirmDialogProps } from './confirm-dialog';

/** KPI display card: icon + label + value + optional delta/breakdown/footer link. */
export { MetricCard } from './metric-card';
export type { MetricCardProps } from './metric-card';

/** Single row in a divide-y activity feed: status dot + label + meta + message + timestamp. */
export { EventFeedItem } from './event-feed-item';
export type { EventFeedItemProps } from './event-feed-item';
