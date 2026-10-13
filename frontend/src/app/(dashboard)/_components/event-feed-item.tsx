/** @tier 3 — dashboard-scoped. One row in the home page's activity feed: status dot, identifier, optional meta, message and timestamp. */
import { ListItem, Stack, StatusDot, Text } from '@clear-route/hades';
import type { OrbitStatus } from '@orbit/components';

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
