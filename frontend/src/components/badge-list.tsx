'use client';

/**
 * @tier 2 — App-level shared (no Orbit domain knowledge). Used by the model
 * tables for capabilities, dialects and modalities.
 *
 * A `ClampedList` of plain outline badges: as many as fit in `maxRows` rows (or a
 * count), the rest behind a "+N more" overlay. Each badge truncates with its
 * full label as the tooltip.
 */
import { ClampedList } from './clamped-list';
import { TruncatingBadge } from './truncating-badge';

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
