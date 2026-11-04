import type { ComponentProps } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { DeliveryGrid } from './delivery-grid';
import type { DeliveryRow } from '../_types';
import { makeDelivery } from '../_types/fixtures';

function many(count: number): DeliveryRow[] {
  return Array.from({ length: count }, (_, i) =>
    makeDelivery({ id: `d${i}`, triggerName: `trigger-${String(i).padStart(2, '0')}` }),
  );
}

/** One place holds the default props, so a rerender can vary just one of them. */
function grid(overrides: Partial<ComponentProps<typeof DeliveryGrid>> = {}) {
  return (
    <DeliveryGrid
      deliveries={[]}
      sourceName={(id) => `source ${id}`}
      onOpen={vi.fn()}
      emptyMessage="No deliveries yet."
      resetKey="all|"
      {...overrides}
    />
  );
}

describe('rows', () => {
  it('names the trigger and the source it arrived through', () => {
    render(grid({ deliveries: [makeDelivery()] }));
    expect(screen.getByText('gh-push')).toBeTruthy();
    expect(screen.getByText('source src-1')).toBeTruthy();
  });

  it('never wraps a uuid: an unnamed trigger shows eight characters', () => {
    render(
      grid({
        deliveries: [
          makeDelivery({ triggerName: null, triggerId: '5c0f7a1e-2b44-4f9e-9d21-77aa0f9b1c33' }),
        ],
      }),
    );
    expect(screen.getByText('5c0f7a1e')).toBeTruthy();
    expect(screen.queryByText('5c0f7a1e-2b44-4f9e-9d21-77aa0f9b1c33')).toBeNull();
  });

  it('says an arrival matched nothing rather than leaving the cell blank', () => {
    render(grid({ deliveries: [makeDelivery({ triggerName: null, triggerId: null })] }));
    expect(screen.getByText('No trigger matched')).toBeTruthy();
  });

  it('reduces a wrapped error chain to its human end', () => {
    render(
      grid({
        deliveries: [
          makeDelivery({
            status: 'failed',
            error:
              'start run: unexpected status from kora-agents: status 500: {"code":"internal_error","message":"failed to start run"}',
          }),
        ],
      }),
    );
    expect(screen.getByText('failed to start run')).toBeTruthy();
    expect(screen.queryByText(/unexpected status from kora-agents/)).toBeNull();
  });

  it('shows the arrival time relatively, with the exact time on hover', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-29T04:30:55Z'));
    render(grid({ deliveries: [makeDelivery()] }));
    const received = screen.getByText('6m ago');
    expect(received.getAttribute('title')).toBe('2026-07-29 04:24:55 UTC');
    vi.useRealTimers();
  });

  it('offers no event badge when the arrival named no event', () => {
    render(grid({ deliveries: [makeDelivery({ eventType: '' })] }));
    expect(screen.queryByText('push')).toBeNull();
  });

  it('opens the delivery when its row is clicked', () => {
    const onOpen = vi.fn();
    render(grid({ deliveries: [makeDelivery()], sourceName: (id) => id, onOpen }));
    fireEvent.click(screen.getByRole('row', { name: /View payload and outcome/ }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});

describe('pagination', () => {
  it('shows one page of twelve by default, and counts the rest', () => {
    render(grid({ deliveries: many(30) }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
    expect(screen.getByText('trigger-11')).toBeTruthy();
    expect(screen.queryByText('trigger-12')).toBeNull();
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
  });

  it('pages forward and back', () => {
    render(grid({ deliveries: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('stops at both ends', () => {
    render(grid({ deliveries: many(30) }));
    expect(screen.getByRole('button', { name: 'Previous page' }).hasAttribute('disabled')).toBe(
      true,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('25–30 of 30')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Next page' }).hasAttribute('disabled')).toBe(true);
  });

  it('returns to page one when the filters change', () => {
    const view = render(grid({ deliveries: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    // A page index means nothing once the rows behind it were replaced.
    view.rerender(grid({ deliveries: many(30), resetKey: 'failed|' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not resurrect the old page when the filters come back', () => {
    const view = render(grid({ deliveries: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    view.rerender(grid({ deliveries: many(30), resetKey: 'failed|' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();

    view.rerender(grid({ deliveries: many(30), resetKey: 'all|' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not strand the viewer on a page the filter emptied', () => {
    const view = render(grid({ deliveries: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    // Same filters, fewer rows: the page index is now out of range. The
    // DataTable resets to page 0 when rerendered with a new key (resetKey
    // change) or when internal pagination clamps the page. With only 3 rows
    // the pagination footer is hidden (single page) — verify the first row is
    // visible rather than checking the footer text.
    view.rerender(grid({ deliveries: many(3) }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('repages when the page size changes', () => {
    render(grid({ deliveries: many(30) }));
    fireEvent.change(screen.getByLabelText('deliveries per page'), { target: { value: '25' } });
    expect(screen.getByText('1–25 of 30')).toBeTruthy();
  });

  it('offers no footer when there is nothing to page', () => {
    render(grid());
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    expect(screen.getByText('No deliveries yet.')).toBeTruthy();
  });
});
