'use client';

/**
 * @tier 2 — App-level shared layout component (no Orbit domain knowledge). Used
 * by `ScopeChipList` and `BadgeList`.
 *
 * A wrapping list of items that shows a bounded number inline and collapses the
 * rest into a "+N more" trigger opening a scrollable overlay with every item, so
 * a table row stays a bounded height however many items it carries. The bound is
 * either an item count (`max`) or, with `maxRows`, however many items fit in
 * that many rows at the width the list is given.
 *
 * @promote-to-tier-1 if: a second surface outside the dashboard needs it; it is
 * a Terra candidate, which needs human approval before any Terra change.
 */
import { Button, Grid, Popover, PopoverContent, PopoverTrigger, Stack, Text } from '@orbit/terra';
import { Fragment, type ReactNode } from 'react';
import { useClampedRows } from './use-clamped-rows';

export interface ClampedListProps<T> {
  items: readonly T[];
  getKey: (item: T) => string;
  /** Renders one item inline. It should truncate (not overflow) when squeezed. */
  renderItem: (item: T) => ReactNode;
  /** What the items are called, for the trigger's accessible name ("Show all 12 scopes"). */
  noun: string;
  /** Items shown before collapsing the rest. Defaults to 3. Ignored when `maxRows` is set. */
  max?: number;
  /** Show as many items as fit in this many rows, then "+N more". Measured, so it adapts to the width. */
  maxRows?: number;
}

export function ClampedList<T>({
  items,
  getKey,
  renderItem,
  noun,
  max = 3,
  maxRows,
}: ClampedListProps<T>) {
  const listRef = useClampedRows<HTMLDivElement>(maxRows ?? Infinity, items.length);

  if (items.length === 0) {
    return (
      <Text size="supporting" color="muted">
        —
      </Text>
    );
  }

  // With `maxRows`, every item is rendered and the hook hides what does not fit;
  // with a count, the surplus is simply not rendered.
  const shown = maxRows === undefined ? items.slice(0, max) : items;
  const hidden = maxRows === undefined ? items.length - shown.length : 0;
  const showTrigger = maxRows !== undefined || hidden > 0;

  return (
    <Stack ref={listRef} direction="horizontal" align="center" gap="tight" wrap>
      {shown.map((item) => (
        // A stretched, shrinkable wrapper gives the item a definite width, so one
        // wider than the cell ends in an ellipsis rather than bleeding into the
        // neighbouring column.
        <Stack key={getKey(item)} align="stretch" minWidth="none" overflow="hidden">
          {renderItem(item)}
        </Stack>
      ))}
      {showTrigger && (
        <Popover>
          <PopoverTrigger asChild>
            <Button
              data-clamp-trigger=""
              variant="muted"
              size="sm"
              aria-label={`Show all ${items.length} ${noun}`}
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
                {items.map((item) => (
                  <Fragment key={getKey(item)}>{renderItem(item)}</Fragment>
                ))}
              </Grid>
            </Stack>
          </PopoverContent>
        </Popover>
      )}
    </Stack>
  );
}
