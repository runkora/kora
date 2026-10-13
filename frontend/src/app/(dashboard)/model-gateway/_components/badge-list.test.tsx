import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BadgeList } from './badge-list';

const values = ['chat', 'embeddings', 'vision', 'tools', 'reasoning', 'caching'];

/** jsdom has no layout: put each element on row `floor(index / perRow)`. */
function mockWrapping(perRow: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
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

describe('BadgeList', () => {
  it('shows a dash when there are no values', () => {
    render(<BadgeList values={[]} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('shows the first few and a +N more trigger in count mode', () => {
    render(<BadgeList values={values} max={2} noun="capabilities" />);
    expect(screen.getByText('chat')).toBeInTheDocument();
    expect(screen.getByText('embeddings')).toBeInTheDocument();
    expect(screen.queryByText('vision')).toBeNull();
    expect(screen.getByRole('button', { name: 'Show all 6 capabilities' })).toHaveTextContent(
      '+4 more',
    );
  });

  it('clamps to rows and lists everything in the overlay', async () => {
    mockWrapping(2);
    const user = userEvent.setup();
    render(<BadgeList values={values} maxRows={2} noun="capabilities" />);
    // Row 1: chat, embeddings. Row 2: vision plus the trigger.
    expect(screen.getByText('vision')).toBeVisible();
    expect(screen.getByText('tools')).not.toBeVisible();
    expect(screen.getByRole('button', { name: 'Show all 6 capabilities' })).toHaveTextContent(
      '+3 more',
    );
    await user.click(screen.getByRole('button', { name: 'Show all 6 capabilities' }));
    expect(screen.getAllByText('caching').at(-1)).toBeVisible();
  });
});
