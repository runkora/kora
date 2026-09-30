/**
 * @tier 2 — App-level shared. Cross-feature; renders a single row in a
 * divide-y activity feed: status dot + primary identifier + optional meta +
 * message + timestamp. Used by dashboard home event spine, model-gateway
 * RecentLogsCard, and any future event feed.
 * @promote-to-tier-1 if: OrbitStatus dependency is removed and a second
 * non-dashboard kora surface needs a generic feed row.
 */
import { ListItem, Stack, StatusDot, Text } from '@orbit/terra';
import type { OrbitStatus } from './status-badge';

export interface EventFeedItemProps {
  status: OrbitStatus;
  /** Primary identifier string, rendered in monospace (e.g. event type, model name). */
  label: string;
  /** Supporting identifier rendered next to the label (e.g. agent name, key id). */
  meta?: string;
  message: string;
  timestamp: string;
}

export function EventFeedItem({ status, label, meta, message, timestamp }: EventFeedItemProps) {
  const dotStatus = status === 'unknown' ? ('muted' as const) : status;
  return (
    <ListItem>
      <Stack
        direction="horizontal"
        align="start"
        gap="snug"
        paddingInline="comfortable"
        paddingBlock="snug"
      >
        <StatusDot status={dotStatus} size="sm" aria-hidden />
        <Stack direction="vertical" gap="tight" grow minWidth="none">
          <Stack direction="horizontal" align="center" gap="snug">
            <Text size="caption" weight="emphasis" mono>
              {label}
            </Text>
            {meta && (
              <>
                <Text size="caption" color="subtle">
                  ·
                </Text>
                <Text size="caption" color="subtle">
                  {meta}
                </Text>
              </>
            )}
          </Stack>
          <Text size="caption" color="muted" truncate>
            {message}
          </Text>
        </Stack>
        <Stack shrink gap="none">
          <Text size="caption" color="subtle" mono>
            {timestamp}
          </Text>
        </Stack>
      </Stack>
    </ListItem>
  );
}
