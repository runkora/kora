import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { AgentsSpendCard } from './agents-spend-card';

const BASE = {
  agents: [{ name: 'Coder', cost: 10 }],
  total: 10,
  periodText: 'last 7 days',
  previousText: 'previous 7 days',
};

const win = (cadence: 'weekly' | 'monthly', limit: number, spent: number) => ({
  cadence,
  limit,
  spent,
  resetsAt: '2026-11-01T00:00:00Z',
});

async function openBudgets() {
  await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));
}

describe('AgentsSpendCard', () => {
  it('shows a bar for each budget window, weekly ones as a week, and a zero limit as spent', async () => {
    renderWithProviders(
      <AgentsSpendCard
        {...BASE}
        budgets={[
          { name: 'Coder', windows: [win('monthly', 100, 40)] },
          { name: 'Triage', windows: [win('weekly', 20, 3), win('monthly', 80, 30)] },
          { name: 'Capped', windows: [win('monthly', 0, 2)] },
        ]}
      />,
    );
    await openBudgets();
    expect(screen.getByText('of $20.00 a week')).toBeInTheDocument();
    expect(screen.getByText('of $80.00')).toBeInTheDocument();
    expect(screen.getByText('Triage (weekly)')).toBeInTheDocument();
    expect(screen.getByText('of $0.00')).toBeInTheDocument();
  });

  it('lists spend of deleted agents as a row, so the rows add up to the total', () => {
    renderWithProviders(<AgentsSpendCard {...BASE} agents={[]} total={6.5} deletedCost={6.5} />);
    expect(screen.getByText('Deleted agents')).toBeInTheDocument();
    expect(screen.queryByText('No spend in this window yet.')).not.toBeInTheDocument();
  });

  describe('at 150 agents', () => {
    const agents = Array.from({ length: 150 }, (_, i) => ({
      name: `Agent ${i + 1}`,
      cost: 150 - i,
    }));
    const total = agents.reduce((n, a) => n + a.cost, 0);

    it('folds the legend into the top agents and Other', () => {
      renderWithProviders(<AgentsSpendCard {...BASE} agents={agents} total={total} />);
      expect(screen.getByText('Other (145)')).toBeInTheDocument();
      expect(screen.queryByText('Agent 7')).not.toBeInTheDocument();
    });

    it('shows eight budget rows, the closest to their limit first, and counts the rest', async () => {
      const budgets = agents.map((a, i) => ({
        name: a.name,
        windows: [win('monthly', 1000 - i, i)],
      }));
      renderWithProviders(
        <AgentsSpendCard {...BASE} agents={agents} total={total} budgets={budgets} />,
      );
      await openBudgets();
      expect(screen.getByText('Agent 150')).toBeInTheDocument();
      expect(screen.queryByText('Agent 1')).not.toBeInTheDocument();
      expect(screen.getByText('+142 more agents not shown')).toBeInTheDocument();
    });
  });
});
