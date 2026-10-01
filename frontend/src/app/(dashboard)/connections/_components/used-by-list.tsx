'use client';

/** @tier 3 — connections-scoped. Promote to src/components/ if a second feature needs this. */

import {
  Badge,
  Button,
  Grid,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  Text,
} from '@orbit/terra';
import { TruncatingBadge, useClampedRows } from '@orbit/components';
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
  const listRef = useClampedRows<HTMLDivElement>(maxRows, usages.length);

  if (usages.length === 0) {
    return (
      <Text size="supporting" color="muted">
        Not attached
      </Text>
    );
  }

  // Every badge is rendered; the hook hides those that do not fit in `maxRows`
  // rows and writes the "+N more" count.
  return (
    <Stack ref={listRef} direction="horizontal" align="center" gap="tight" wrap minWidth="none">
      {usages.map((usage) => (
        <TruncatingBadge
          key={usage.slug}
          label={usage.name}
          variant="outline"
          textColor="foreground"
        />
      ))}
      <Popover>
        <PopoverTrigger asChild>
          <Button
            data-clamp-trigger=""
            variant="muted"
            size="sm"
            aria-label={`Show all ${usages.length} agents`}
            onClick={(e) => e.stopPropagation()}
          ></Button>
        </PopoverTrigger>
        <PopoverContent align="start" onClick={(e) => e.stopPropagation()}>
          {/* Grid inside the scroller: flex children would shrink to fit
              maxHeight and overlap; grid items keep their natural height. */}
          <Stack padding="snug" maxHeight="scroll-md" overflowY="auto">
            <Grid cols={1} gap="tight">
              {usages.map((usage) => (
                <Badge key={usage.slug} variant="outline" size="sm">
                  {usage.name}
                </Badge>
              ))}
            </Grid>
          </Stack>
        </PopoverContent>
      </Popover>
    </Stack>
  );
}
