import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ScopeChipList } from './scope-chip-list';
import { clampRows, rowIndices } from './use-clamped-rows';

/**
 * jsdom has no layout. Simulate a wrapping row: each element's box sits on row
 * `floor(indexAmongSiblings / perRow)`, so `perRow` items fit per line.
 */
function mockWrapping(perRow: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    // Like a browser, ignore siblings that are display:none.
    const siblings = (this.parentElement ? Array.from(this.parentElement.children) : [this]).filter(
      (el) => (el as HTMLElement).style.display !== 'none',
    );
    const row = Math.floor(siblings.indexOf(this) / perRow);
    return {
      top: row * 30,
      height: 20,
      bottom: row * 30 + 20,
      left: 0,
      right: 0,
      width: this.hasAttribute('data-clamp-trigger') ? 80 : 0,
      x: 0,
      y: 0,
    } as DOMRect;
  });
}

afterEach(() => vi.restoreAllMocks());

describe('rowIndices', () => {
  it('groups boxes sharing a centre line into one row', () => {
    expect(rowIndices([10, 10, 10, 40, 40, 70])).toEqual([0, 0, 0, 1, 1, 2]);
  });

  it('tolerates children of different heights that share a centre', () => {
    expect(rowIndices([10, 12, 9, 41])).toEqual([0, 0, 0, 1]);
  });

  it('handles an empty list', () => {
    expect(rowIndices([])).toEqual([]);
  });
});

describe('ScopeChipList maxRows', () => {
  const scopes = Array.from({ length: 10 }, (_, i) => `res:thing${i}:read`);

  it('shows every chip, and no trigger, when they fit in the allowed rows', () => {
    mockWrapping(5); // 10 chips => 2 rows
    render(<ScopeChipList scopes={scopes} maxRows={2} />);
    expect(screen.getByText('thing9')).toBeVisible();
    expect(screen.getByRole('button', { hidden: true })).not.toBeVisible();
  });

  it('keeps two rows and reserves room for "+N more" on the last one', () => {
    mockWrapping(3); // 10 chips => 4 rows; the trigger must also fit in row 2
    render(<ScopeChipList scopes={scopes} maxRows={2} />);
    // Row 1: chips 0-2. Row 2: chips 3-4 plus the trigger (3 per row).
    expect(screen.getByText('thing4')).toBeVisible();
    expect(screen.getByText('thing5')).not.toBeVisible();
    const trigger = screen.getByRole('button', { name: 'Show all 10 scopes' });
    expect(trigger).toBeVisible();
    expect(trigger).toHaveTextContent('+5 more');
  });

  it('falls back to the chip count when maxRows is not given', () => {
    mockWrapping(3);
    render(<ScopeChipList scopes={scopes} max={4} />);
    expect(screen.getByRole('button')).toHaveTextContent('+6 more');
    expect(screen.queryByText('thing4')).toBeNull(); // not rendered at all
  });
});

describe('clampRows trigger reservation', () => {
  function build(itemCount: number) {
    const container = document.createElement('div');
    for (let i = 0; i < itemCount; i += 1) container.appendChild(document.createElement('span'));
    const trigger = document.createElement('button');
    trigger.setAttribute('data-clamp-trigger', '');
    container.appendChild(trigger);
    document.body.appendChild(container);
    Object.defineProperty(container, 'clientWidth', { value: 300 });
    return {
      container,
      trigger,
      items: Array.from(container.children).slice(0, itemCount) as HTMLElement[],
    };
  }

  it('caps each item so the widest trigger can share the last row', () => {
    mockWrapping(2);
    const { container, items } = build(6);
    clampRows(container, 2);
    // 300 container - 80 trigger - 0 gap (jsdom has no computed gap)
    expect(items[0].style.maxWidth).toBe('220px');
    container.remove();
  });

  it('does not cap items when everything already fits', () => {
    mockWrapping(6);
    const { container, items } = build(4);
    clampRows(container, 2);
    expect(items[0].style.maxWidth).toBe('');
    container.remove();
  });
});
