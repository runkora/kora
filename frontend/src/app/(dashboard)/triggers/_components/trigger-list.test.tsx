import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { TriggerList } from './trigger-list';
import type { TriggerRow } from '../_types';

function trigger(overrides: Partial<TriggerRow> = {}): TriggerRow {
  return {
    id: 't1',
    kind: 'webhook',
    sourceId: 'src-1',
    name: 'gh-push',
    agentTypeId: 'at-1',
    agentSlug: 'coder',
    eventType: 'push',
    matchCondition: {},
    promptTemplate: 'go',
    enabled: true,
    createdAt: '2026-07-01T00:00:00Z',
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,
    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,
    consecutiveFailures: 0,
    autoPausedAt: null,
    pendingCount: 0,
    ...overrides,
  };
}

function many(count: number): TriggerRow[] {
  return Array.from({ length: count }, (_, i) =>
    trigger({ id: `t${i}`, name: `trigger-${String(i).padStart(2, '0')}` }),
  );
}

function renderList(triggers: TriggerRow[], resetKey = 'all|') {
  return render(
    <TriggerList
      triggers={triggers}
      deliveries={[]}
      sourceName={(id) => id}
      onOpen={vi.fn()}
      onToggle={vi.fn()}
      emptyMessage="No triggers yet."
      resetKey={resetKey}
    />,
  );
}

describe('pagination', () => {
  it('shows one page of twelve by default, and counts the rest', () => {
    renderList(many(30));
    expect(screen.getByText('trigger-00')).toBeTruthy();
    expect(screen.getByText('trigger-11')).toBeTruthy();
    expect(screen.queryByText('trigger-12')).toBeNull();
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
  });

  it('pages forward and back', () => {
    renderList(many(30));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(screen.getByText('trigger-12')).toBeTruthy();
    expect(screen.queryByText('trigger-00')).toBeNull();
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('stops at both ends', () => {
    renderList(many(30));
    expect(screen.getByRole('button', { name: 'Previous page' }).hasAttribute('disabled')).toBe(
      true,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('25–30 of 30')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Next page' }).hasAttribute('disabled')).toBe(true);
  });

  it('returns to page one when the filters change', () => {
    const view = renderList(many(30), 'all|');
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    // A page index means nothing once the rows behind it were replaced.
    view.rerender(
      <TriggerList
        triggers={many(30)}
        deliveries={[]}
        sourceName={(id) => id}
        onOpen={vi.fn()}
        onToggle={vi.fn()}
        emptyMessage="No triggers yet."
        resetKey="webhook|push"
      />,
    );
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not resurrect the old page when the filters come back', () => {
    const view = renderList(many(30), 'all|');
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    const list = (resetKey: string) => (
      <TriggerList
        triggers={many(30)}
        deliveries={[]}
        sourceName={(id) => id}
        onOpen={vi.fn()}
        onToggle={vi.fn()}
        emptyMessage="No triggers yet."
        resetKey={resetKey}
      />
    );

    view.rerender(list('webhook|push'));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();

    // Back to the first filter: the reset was written back, so page 3 is gone
    // rather than waiting behind the old key.
    view.rerender(list('all|'));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not strand the viewer on a page the filter emptied', () => {
    const view = renderList(many(30), 'all|');
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    // Same filters, fewer rows: the page index is now out of range.
    view.rerender(
      <TriggerList
        triggers={many(3)}
        deliveries={[]}
        sourceName={(id) => id}
        onOpen={vi.fn()}
        onToggle={vi.fn()}
        emptyMessage="No triggers yet."
        resetKey="all|"
      />,
    );
    expect(screen.getByText('1–3 of 3')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('repages when the page size changes', () => {
    renderList(many(30));
    fireEvent.change(screen.getByLabelText('triggers per page'), { target: { value: '25' } });
    expect(screen.getByText('1–25 of 30')).toBeTruthy();
  });

  it('offers no footer when there is nothing to page', () => {
    renderList([]);
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    expect(screen.getByText('No triggers yet.')).toBeTruthy();
  });
});

describe('auto-paused', () => {
  it('says so on the row, because only the platform pausing it needs looking into', () => {
    renderList([trigger({ autoPausedAt: '2026-07-28T00:00:00Z' })]);
    expect(screen.getByText('Auto-paused')).toBeTruthy();
  });

  it('says nothing extra for a trigger a person turned off', () => {
    // The toggle already reports that one.
    renderList([trigger({ enabled: false })]);
    expect(screen.queryByText('Auto-paused')).toBeNull();
  });
});
