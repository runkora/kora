'use client';

/**
 * @tier 2 — kora-domain-aware: run starters and trigger kinds. Who started a run as one round
 * mark. Used by: the agents pages' run rows, the Runs page.
 */

import {
  Avatar,
  Icon,
  Stack,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  type IconName,
} from '@runkora/hades';
import type { TriggerKind } from '@runkora/shared/api';

const TRIGGER_KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'zap',
  scheduled: 'clock',
  manual: 'play',
  event: 'activity',
};

/** The icon for a trigger of the given kind; `zap` for a run recorded without one. */
export function triggerKindIcon(kind: TriggerKind | null): IconName {
  return (kind && TRIGGER_KIND_ICON[kind]) ?? 'zap';
}

/** The parts of a run row that say who started it. */
export interface StarterMarkRun {
  createdBy: string;
  createdByName: string | null;
  startedByTrigger: {
    triggerId: string;
    triggerName: string | null;
    triggerKind: TriggerKind | null;
  } | null;
}

export interface StarterMarkProps {
  row: StarterMarkRun;
}

/** Who started the run as one round mark: a person's initials or the trigger kind's icon. */
export function StarterMark({ row }: StarterMarkProps) {
  const trigger = row.startedByTrigger;
  const name = trigger
    ? (trigger.triggerName ?? trigger.triggerId)
    : (row.createdByName ?? row.createdBy);
  const tooltip = `Started by ${name}`;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Stack shrink aria-label={tooltip}>
          {trigger ? (
            <Icon
              name={triggerKindIcon(trigger.triggerKind)}
              size="2xs"
              container
              circle
              containerVariant="accent"
            />
          ) : (
            <Avatar name={name} size="2xs" />
          )}
        </Stack>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
