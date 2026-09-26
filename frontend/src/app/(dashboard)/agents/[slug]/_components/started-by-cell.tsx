'use client';

/** @tier 3 — agents/[slug]-scoped. "Started by" cell shared by the Runs and Overview tables. */

import { Badge, Icon, Stack, Text } from '@orbit/terra';
import type { RunRow } from '../../_types';

interface StartedByCellProps {
  row: RunRow;
  showDelegatedBadge?: boolean;
}

export function StartedByCell({ row, showDelegatedBadge = true }: StartedByCellProps) {
  return (
    <Stack direction="horizontal" align="center" gap="tight">
      {row.startedByTrigger ? (
        <>
          <Icon name="zap" size="2xs" color="accent" label="" />
          <Text size="supporting" color="muted">
            {row.startedByTrigger.triggerName ?? row.startedByTrigger.triggerId}
          </Text>
        </>
      ) : (
        <Text size="supporting" color="muted">
          {row.createdByName ?? row.createdBy}
        </Text>
      )}
      {showDelegatedBadge && row.delegated && (
        <Badge variant="default" size="sm">
          Delegated
        </Badge>
      )}
    </Stack>
  );
}
