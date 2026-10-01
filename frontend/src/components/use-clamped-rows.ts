'use client';

/**
 * @tier 2 — App-level shared layout hook (no Orbit domain knowledge; it is a
 * hook, not a component, so it stays out of Terra). Used by: `ScopeChipList`,
 * connections' `UsedByList`.
 *
 * Lets a wrapping list show as many items as fit in `maxRows` rows and
 * collapse the rest into a trailing "+N more" trigger. How many fit depends on
 * the width the container is given, so it is measured, not counted.
 *
 * Contract for the container behind the returned ref: its children are every
 * item in order, followed by one trigger element marked `data-clamp-trigger`
 * and rendered with no children of its own. Items that do not fit are hidden
 * and the trigger's text is written ("+N more"), directly on the DOM (no React
 * state: this is a measure-then-mutate loop, like fitting text to a width).
 */
import { useLayoutEffect, useRef } from 'react';

export const CLAMP_TRIGGER_ATTR = 'data-clamp-trigger';

/**
 * Row index of each box, from its vertical centre. Centres, not tops, because
 * boxes of different heights are centred on the same line. `centres` must be
 * in document order (left to right, top to bottom).
 */
export function rowIndices(centres: readonly number[], tolerance = 6): number[] {
  const rows: number[] = [];
  let row = 0;
  let rowCentre = centres[0] ?? 0;
  centres.forEach((centre, i) => {
    if (i > 0 && centre - rowCentre > tolerance) {
      row += 1;
      rowCentre = centre;
    }
    rows.push(row);
  });
  return rows;
}

function rowsOf(elements: readonly HTMLElement[]): number[] {
  return rowIndices(
    elements.map((element) => {
      const box = element.getBoundingClientRect();
      return box.top + box.height / 2;
    }),
  );
}

/** The widest count the trigger is expected to show, so its reserved room never grows. */
const WIDEST_TRIGGER_LABEL = '+999 more';

/**
 * Shows as many items as fit in `maxRows` rows and hides the rest behind the
 * trigger. When the list overflows, each item is also capped so that the
 * trigger (up to a three-digit count) can always share the last row with it;
 * without the cap a single wide item fills its row and the trigger has
 * nowhere to go.
 */
export function clampRows(container: HTMLElement, maxRows: number): void {
  const children = Array.from(container.children) as HTMLElement[];
  const trigger = children.find((child) => child.hasAttribute(CLAMP_TRIGGER_ATTR));
  const items = children.filter((child) => child !== trigger);

  // Start from everything shown and uncapped, so a wider container can win
  // items back and an item that fits is never truncated for no reason.
  items.forEach((item) => {
    item.style.removeProperty('display');
    item.style.removeProperty('max-width');
  });
  if (trigger) trigger.style.display = 'none';

  if (items.length === 0 || !trigger) return;
  let itemRows = rowsOf(items);
  if (itemRows[itemRows.length - 1] < maxRows) return; // everything fits

  // Overflowing: cap the items at what leaves room for the widest trigger.
  trigger.textContent = WIDEST_TRIGGER_LABEL;
  trigger.style.display = '';
  const gap = parseFloat(getComputedStyle(container).columnGap) || 0;
  const triggerWidth = trigger.getBoundingClientRect().width;
  const cap = container.clientWidth - triggerWidth - gap;
  trigger.style.display = 'none';
  if (cap > 0) {
    items.forEach((item) => {
      item.style.maxWidth = `${cap}px`;
    });
    itemRows = rowsOf(items);
  }

  const show = (visible: number) => {
    items.forEach((item, i) => {
      item.style.display = i < visible ? '' : 'none';
    });
    trigger.textContent = `+${items.length - visible} more`;
    trigger.style.display = '';
  };

  let visible = itemRows.filter((row) => row < maxRows).length;
  show(visible);
  // The trigger needs room too: drop items until it sits within `maxRows`.
  while (visible > 0) {
    const rows = rowsOf([...items.slice(0, visible), trigger]);
    if (rows[rows.length - 1] < maxRows) break;
    visible -= 1;
    show(visible);
  }
}

/**
 * Attach the returned ref to the list container. Re-fits on mount, whenever
 * `contentKey` or `maxRows` changes, when the container's width changes, and
 * once web fonts finish loading (chips measured against the fallback font are
 * narrower than they end up).
 */
export function useClampedRows<T extends HTMLElement>(maxRows: number, contentKey: unknown) {
  const ref = useRef<T>(null);

  useLayoutEffect(() => {
    const container = ref.current;
    if (!container || !Number.isFinite(maxRows)) return;

    const fit = () => clampRows(container, maxRows);
    fit();

    let cancelled = false;
    document.fonts?.ready.then(() => {
      if (!cancelled) fit();
    });

    let lastWidth = container.clientWidth;
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(() => {
        // Only a width change alters the answer; our own hiding changes height.
        if (container.clientWidth !== lastWidth) {
          lastWidth = container.clientWidth;
          fit();
        }
      });
      observer.observe(container);
    }

    return () => {
      cancelled = true;
      observer?.disconnect();
    };
  }, [maxRows, contentKey]);

  return ref;
}
