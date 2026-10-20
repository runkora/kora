import type { ComponentProps } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders as render } from '@runkora/test-utils';

import { TriggerList } from './trigger-list';
import type { TriggerRow } from '../_types';
import { makeTrigger } from '../_types/fixtures';

function many(count: number): TriggerRow[] {
  return Array.from({ length: count }, (_, i) =>
    makeTrigger({ id: `t${i}`, name: `trigger-${String(i).padStart(2, '0')}` }),
  );
}

/** One place holds the default props, so a rerender can vary just one of them. */
function list(overrides: Partial<ComponentProps<typeof TriggerList>> = {}) {
  return (
    <TriggerList
      triggers={[]}
      deliveries={[]}
      sourceName={(id) => id}
      onOpen={vi.fn()}
      onToggle={vi.fn()}
      canRunAgent={() => true}
      emptyMessage="No triggers yet."
      resetKey="all|"
      {...overrides}
    />
  );
}

describe('pagination', () => {
  it('shows one page of twelve by default, and counts the rest', () => {
    render(list({ triggers: many(30) }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
    expect(screen.getByText('trigger-11')).toBeTruthy();
    expect(screen.queryByText('trigger-12')).toBeNull();
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
  });

  it('pages forward and back', () => {
    render(list({ triggers: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(screen.getByText('trigger-12')).toBeTruthy();
    expect(screen.queryByText('trigger-00')).toBeNull();
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('stops at both ends', () => {
    render(list({ triggers: many(30) }));
    expect(screen.getByRole('button', { name: 'Previous page' }).hasAttribute('disabled')).toBe(
      true,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('25–30 of 30')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Next page' }).hasAttribute('disabled')).toBe(true);
  });

  it('returns to page one when the filters change', () => {
    const view = render(list({ triggers: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    // A page index means nothing once the rows behind it were replaced.
    view.rerender(list({ triggers: many(30), resetKey: 'webhook|push' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not resurrect the old page when the filters come back', () => {
    const view = render(list({ triggers: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('13–24 of 30')).toBeTruthy();

    view.rerender(list({ triggers: many(30), resetKey: 'webhook|push' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();

    // Back to the first filter: the reset was written back, so page 3 is gone
    // rather than waiting behind the old key.
    view.rerender(list({ triggers: many(30), resetKey: 'all|' }));
    expect(screen.getByText('1–12 of 30')).toBeTruthy();
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('does not strand the viewer on a page the filter emptied', () => {
    const view = render(list({ triggers: many(30) }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    // Same filters, fewer rows: the page index is now out of range. The
    // DataTable resets to page 0 when rerendered with a new key (resetKey
    // change) or when internal pagination clamps the page. With only 3 rows
    // the pagination footer is hidden (single page) — verify the first row is
    // visible rather than checking the footer text.
    view.rerender(list({ triggers: many(3) }));
    expect(screen.getByText('trigger-00')).toBeTruthy();
  });

  it('repages when the page size changes', () => {
    render(list({ triggers: many(30) }));
    fireEvent.change(screen.getByLabelText('triggers per page'), { target: { value: '25' } });
    expect(screen.getByText('1–25 of 30')).toBeTruthy();
  });

  it('offers no footer when there is nothing to page', () => {
    render(list());
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    expect(screen.getByText('No triggers yet.')).toBeTruthy();
  });
});

describe('auto-paused', () => {
  it('says so on the row, because only the platform pausing it needs looking into', () => {
    render(list({ triggers: [makeTrigger({ autoPausedAt: '2026-07-28T00:00:00Z' })] }));
    expect(screen.getByText('Auto-paused')).toBeTruthy();
  });

  it('says nothing extra for a trigger a person turned off', () => {
    // The toggle already reports that one.
    render(list({ triggers: [makeTrigger({ enabled: false })] }));
    expect(screen.queryByText('Auto-paused')).toBeNull();
  });
});

describe('run authority', () => {
  const routed = makeTrigger({
    id: 'r1',
    name: 'routed',
    targetKind: 'routed',
    agentTypeId: '',
    routing: {
      candidates: ['at-a', 'at-b'],
      model: 'liquid/d1',
      confidence: 'balanced',
      fallbackAgentTypeId: null,
      joinRuns: true,
      joinKey: '',
    },
  });

  it('lets a routed trigger be toggled only when every candidate can run', () => {
    const view = render(list({ triggers: [routed], canRunAgent: (id) => id === 'at-a' }));
    expect(screen.getByLabelText('Disable routed').hasAttribute('disabled')).toBe(true);

    view.rerender(list({ triggers: [routed], canRunAgent: () => true }));
    expect(screen.getByLabelText('Disable routed').hasAttribute('disabled')).toBe(false);
  });
});

describe('chat command triggers', () => {
  const chat = makeTrigger({
    id: 'c1',
    name: 'answer-mentions',
    sourceId: 'slack-src',
    eventType: 'mention',
  });
  const hook = makeTrigger({ id: 'w1', name: 'on-push', sourceId: 'gh-src', eventType: 'push' });
  const category = (id: string) => (id === 'slack-src' ? 'chat' : 'webhook') as 'chat' | 'webhook';

  it('shows the chat event by its label and a webhook event as its type', () => {
    render(list({ triggers: [chat, hook], sourceCategory: category }));
    expect(screen.getByText('Mention')).toBeTruthy();
    expect(screen.getByText('push')).toBeTruthy();
  });

  it('finds a chat command by the label it shows', () => {
    const dm = makeTrigger({
      id: 'c2',
      name: 'dm-helper',
      sourceId: 'slack-src',
      eventType: 'direct_message',
    });
    render(list({ triggers: [dm, hook], sourceCategory: category }));
    fireEvent.change(screen.getByPlaceholderText('Search triggers…'), {
      target: { value: 'direct message' },
    });
    expect(screen.getByText('dm-helper')).toBeTruthy();
    expect(screen.queryByText('on-push')).toBeNull();
  });

  it('reads every trigger as a webhook when the caller knows no categories', () => {
    render(list({ triggers: [chat] }));
    expect(screen.getByText('mention')).toBeTruthy();
  });
});
