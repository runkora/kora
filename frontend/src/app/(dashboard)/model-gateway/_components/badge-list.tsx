'use client';

/**
 * @tier 3 — model-gateway-scoped. Used by the model tables for capabilities and
 * dialects. Promote to src/components/ if a second feature needs it.
 *
 * A `ClampedList` of plain outline badges: as many as fit in `maxRows` rows (or a
 * count), the rest behind a "+N more" overlay. Each badge truncates with its
 * full label as the tooltip.
 */
import { ClampedList } from '@runkora/components';
import { TruncatingBadge } from '@runkora/hades';

export interface BadgeListProps {
  values: readonly string[];
  /** What the values are called, for the trigger's accessible name. Defaults to "values". */
  noun?: string;
  /** Badges shown before collapsing the rest. Defaults to 3. Ignored when `maxRows` is set. */
  max?: number;
  /** Show as many badges as fit in this many rows, then "+N more". */
  maxRows?: number;
}

export function BadgeList({ values, noun = 'values', max, maxRows }: BadgeListProps) {
  return (
    <ClampedList
      items={values}
      getKey={(value) => value}
      renderItem={(value) => (
        <TruncatingBadge label={value} variant="outline" textColor="foreground" />
      )}
      noun={noun}
      max={max}
      maxRows={maxRows}
    />
  );
}
