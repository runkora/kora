import { afterEach, describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Stack } from '@orbit/terra';
import { UsedByList } from './used-by-list';

const usages = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot'].map((name) => ({
  slug: name,
  name: `Agent ${name}`,
}));

/** jsdom has no layout: put each element on row `floor(index / perRow)`. */
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
      width: 0,
      x: 0,
      y: 0,
    } as DOMRect;
  });
}

afterEach(() => vi.restoreAllMocks());

describe('UsedByList', () => {
  it('shows "Not attached" when empty', () => {
    render(<UsedByList usages={[]} />);
    expect(screen.getByText('Not attached')).toBeInTheDocument();
  });

  it('shows every agent when they fit in the allowed rows', () => {
    mockWrapping(3); // 3 agents => 1 row
    render(<UsedByList usages={usages.slice(0, 3)} maxRows={2} />);
    expect(screen.getByText('Agent charlie')).toBeVisible();
    expect(screen.getByRole('button', { hidden: true })).not.toBeVisible();
  });

  it('clamps to two rows, keeping room for "+N more", and lists all in the overlay', async () => {
    mockWrapping(2); // 2 per row; the trigger must fit in row 2
    const user = userEvent.setup();
    render(<UsedByList usages={usages} maxRows={2} />);
    // Row 1: alpha, bravo. Row 2: charlie plus the trigger.
    expect(screen.getByText('Agent charlie')).toBeVisible();
    expect(screen.getByText('Agent delta')).not.toBeVisible();
    expect(screen.getByText('+3 more')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Show all 6 agents' }));
    for (const u of usages) expect(screen.getAllByText(u.name).length).toBeGreaterThan(0);
    expect(screen.getAllByText('Agent delta').at(-1)).toBeVisible(); // visible in the overlay
  });

  it('exposes the full name as a tooltip on each inline badge', () => {
    mockWrapping(3);
    const { container } = render(<UsedByList usages={usages.slice(0, 1)} />);
    expect(container.querySelector('[title="Agent alpha"]')).not.toBeNull();
  });

  it('does not fire a parent click when opening or using the overlay', async () => {
    mockWrapping(2);
    const user = userEvent.setup();
    const onRowClick = vi.fn();
    render(
      <Stack onClick={onRowClick}>
        <UsedByList usages={usages} maxRows={1} />
      </Stack>,
    );
    await user.click(screen.getByRole('button', { name: 'Show all 6 agents' }));
    await user.click(screen.getAllByText('Agent echo').at(-1)!);
    expect(onRowClick).not.toHaveBeenCalled();
  });
});
