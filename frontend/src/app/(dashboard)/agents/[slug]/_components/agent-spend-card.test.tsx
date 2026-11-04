import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { AgentSpendCard } from './agent-spend-card';

const BASE = {
  models: [{ name: 'claude-sonnet-4-5', cost: 10 }],
  trend: { labels: ['Mon'], values: [10] },
  total: 10,
  previousTotal: 8,
  change: 25,
  periodText: 'last 7 days',
  previousText: 'previous 7 days',
};

describe('AgentSpendCard', () => {
  it('offers the Budget view only when the agent has a budget', () => {
    const { rerender } = renderWithProviders(<AgentSpendCard {...BASE} budget={null} />);
    expect(screen.queryByRole('button', { name: 'Budget' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Trend' })).toBeTruthy();

    rerender(
      <AgentSpendCard
        {...BASE}
        budget={{
          name: 'Coder',
          windows: [
            { cadence: 'monthly', limit: 100, spent: 40, resetsAt: '2026-11-01T00:00:00Z' },
          ],
        }}
      />,
    );
    expect(screen.getByRole('button', { name: 'Budget' })).toBeTruthy();
  });

  it('says so when the window has no spend', () => {
    renderWithProviders(
      <AgentSpendCard {...BASE} total={0} previousTotal={0} change={null} budget={null} />,
    );
    expect(screen.getByText('No spend in this window yet.')).toBeTruthy();
  });
});
