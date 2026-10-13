'use client';

/** @tier 3 — agents/[slug]-scoped. "Started by" cell shared by the Runs and Overview tables. */

import { Badge, Icon, Stack, Text } from '@orbit/hades';
import type { RunRow } from '../../_types';

interface StartedByCellProps {
  row: RunRow;
}

export function StartedByCell({ row }: StartedByCellProps) {
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
      {row.delegated && (
        <Badge variant="default" size="sm">
          Delegated
        </Badge>
      )}
    </Stack>
  );
}
