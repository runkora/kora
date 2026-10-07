'use client';

/**
 * @tier 2 — Orbit-domain-aware: renders Orbit scope strings via `ScopeChip`.
 * Used by: policy (grants, roles, overview), connections.
 *
 * A `ClampedList` of scope chips: a bounded number inline (a count, or as many
 * as fit in `maxRows` rows) and the rest behind a "+N more" overlay.
 */
import { ClampedList } from './clamped-list';
import { ScopeChip } from './scope-chip';

export interface ScopeChipListProps {
  scopes: readonly string[];
  /** Chips shown inline before collapsing the rest. Defaults to 3. Ignored when `maxRows` is set. */
  max?: number;
  /** Show as many chips as fit in this many rows, then "+N more". Measured, so it adapts to the width. */
  maxRows?: number;
}

export function ScopeChipList({ scopes, max, maxRows }: ScopeChipListProps) {
  return (
    <ClampedList
      items={scopes}
      getKey={(scope) => scope}
      renderItem={(scope) => <ScopeChip scope={scope} />}
      noun="scopes"
      max={max}
      maxRows={maxRows}
    />
  );
}
