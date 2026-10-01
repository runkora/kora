'use client';

/**
 * @tier 2 — Orbit-domain-aware: renders Orbit scope strings via `ScopeChip`.
 * Used by: policy (grants, roles, overview), connections.
 *
 * Shows a bounded number of chips inline; the remainder collapses into a "+N
 * more" trigger that opens a scrollable overlay with every scope. Keeps table
 * rows a bounded height however many scopes a grant or role carries. The bound
 * is either a chip count (`max`) or, with `maxRows`, however many chips fit in
 * that many rows at the width the list is given.
 */
import { Button, Grid, Popover, PopoverContent, PopoverTrigger, Stack, Text } from '@orbit/terra';
import { ScopeChip } from './scope-chip';
import { useClampedRows } from './use-clamped-rows';

export interface ScopeChipListProps {
  scopes: readonly string[];
  /** Chips shown inline before collapsing the rest. Defaults to 3. Ignored when `maxRows` is set. */
  max?: number;
  /** Show as many chips as fit in this many rows, then "+N more". Measured, so it adapts to the width. */
  maxRows?: number;
}

export function ScopeChipList({ scopes, max = 3, maxRows }: ScopeChipListProps) {
  const listRef = useClampedRows<HTMLDivElement>(maxRows ?? Infinity, scopes.length);

  if (scopes.length === 0) {
    return (
      <Text size="supporting" color="muted">
        —
      </Text>
    );
  }

  // With `maxRows`, every chip is rendered and the hook hides what does not fit;
  // with a count, the surplus is simply not rendered.
  const shown = maxRows === undefined ? scopes.slice(0, max) : scopes;
  const hidden = maxRows === undefined ? scopes.length - shown.length : 0;
  const showTrigger = maxRows !== undefined || hidden > 0;

  return (
    <Stack ref={listRef} direction="horizontal" align="center" gap="tight" wrap>
      {shown.map((scope) => (
        // A stretched, shrinkable wrapper gives the chip a definite width, so a
        // scope wider than the cell ends in an ellipsis rather than bleeding
        // into the neighbouring column.
        <Stack key={scope} align="stretch" minWidth="none" overflow="hidden">
          <ScopeChip scope={scope} />
        </Stack>
      ))}
      {showTrigger && (
        <Popover>
          <PopoverTrigger asChild>
            <Button
              data-clamp-trigger=""
              variant="muted"
              size="sm"
              aria-label={`Show all ${scopes.length} scopes`}
              // The list often sits in a clickable table row; opening the overlay
              // must not also trigger the row's action.
              onClick={(e) => e.stopPropagation()}
            >
              {maxRows === undefined ? `+${hidden} more` : null}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" onClick={(e) => e.stopPropagation()}>
            {/* Grid inside the scroller: flex children would shrink to fit maxHeight
                and overlap; grid items keep their natural height. */}
            <Stack padding="snug" maxHeight="scroll-md" overflowY="auto">
              <Grid cols={1} gap="tight">
                {scopes.map((scope) => (
                  <ScopeChip key={scope} scope={scope} />
                ))}
              </Grid>
            </Stack>
          </PopoverContent>
        </Popover>
      )}
    </Stack>
  );
}
