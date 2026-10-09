'use client';

/** @tier 3 — connections-scoped. Promote to src/components/ if a second feature needs this. */

import { Text, TruncatingBadge } from '@orbit/hades';
import { ClampedList } from '@orbit/components';
import type { ConnectionUsage } from '../_types';

/**
 * The agents that attach a connection. Shows as many as fit in `maxRows` rows
 * (each badge truncating to the column with a hover tooltip); the rest collapse
 * into a "+N more" trigger opening a scrollable overlay with every agent, so a
 * connection used by many agents keeps a bounded row height.
 */
export function UsedByList({
  usages,
  maxRows = 2,
}: {
  usages: readonly ConnectionUsage[];
  maxRows?: number;
}) {
  if (usages.length === 0) {
    return (
      <Text size="supporting" color="muted">
        Not attached
      </Text>
    );
  }

  return (
    <ClampedList
      items={usages}
      getKey={(usage) => usage.slug}
      renderItem={(usage) => (
        <TruncatingBadge label={usage.name} variant="outline" textColor="foreground" />
      )}
      noun="agents"
      maxRows={maxRows}
    />
  );
}
