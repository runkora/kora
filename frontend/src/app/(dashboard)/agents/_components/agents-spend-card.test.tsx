import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { AgentsSpendCard } from './agents-spend-card';
import { AgentStatTiles } from './agent-stat-tiles';

const BASE = {
  agents: [{ name: 'Coder', cost: 10 }],
  total: 10,
  periodText: 'last 7 days',
  previousText: 'previous 7 days',
};

describe('AgentsSpendCard', () => {
  it('states a weekly budget as a weekly figure and a monthly one as the month', async () => {
    renderWithProviders(
      <AgentsSpendCard
        {...BASE}
        budgets={[
          { name: 'Coder', spent: 40, limit: 100 },
          { name: 'Triage', spent: 3, limit: 20, cadence: 'weekly' },
          { name: 'Notes', spent: 1 },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));

    expect(screen.getByText('of $100.00')).toBeInTheDocument();
    expect(screen.getByText('of $20.00 a week')).toBeInTheDocument();
    expect(screen.getByText('no budget')).toBeInTheDocument();
    expect(screen.getByText('This month, or this week for weekly budgets')).toBeInTheDocument();
  });

  it('names a weekly tenant cap as weekly', () => {
    renderWithProviders(
      <AgentsSpendCard
        {...BASE}
        tenantBudget={{ limit: 100, spent: 25, daysLeft: 1, cadence: 'weekly' }}
      />,
    );
    expect(screen.getByText('Weekly budget')).toBeInTheDocument();
    expect(screen.getByText(/1 day to go/)).toBeInTheDocument();
  });

  it('shows the state text instead of "no spend" when the window is not loaded', () => {
    renderWithProviders(
      <AgentsSpendCard {...BASE} agents={[]} total={0} emptyText="Loading spend…" />,
    );
    expect(screen.getByText('Loading spend…')).toBeInTheDocument();
    expect(screen.queryByText('No spend in this window yet.')).not.toBeInTheDocument();
  });
});

describe('AgentsSpendCard at 150 agents', () => {
  const agents = Array.from({ length: 150 }, (_, i) => ({ name: `Agent ${i + 1}`, cost: 150 - i }));
  const total = agents.reduce((n, a) => n + a.cost, 0);

  it('lists the top agents and folds the rest into Other', () => {
    renderWithProviders(<AgentsSpendCard {...BASE} agents={agents} total={total} />);
    expect(screen.getByText('Agent 1')).toBeInTheDocument();
    expect(screen.getByText('Other (145)')).toBeInTheDocument();
    expect(screen.queryByText('Agent 7')).not.toBeInTheDocument();
  });

  it('shows eight budget rows, the closest to their limit first, and counts the rest', async () => {
    const budgets = agents.map((a, i) => ({ name: a.name, spent: i, limit: 1000 - i }));
    renderWithProviders(
      <AgentsSpendCard {...BASE} agents={agents} total={total} budgets={budgets} />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));

    // The last agent has spent 149 of a 851 limit, the highest share.
    expect(screen.getByText('Agent 150')).toBeInTheDocument();
    expect(screen.queryByText('Agent 1')).not.toBeInTheDocument();
    expect(screen.getByText('+142 more agents not shown')).toBeInTheDocument();
  });
});

describe('AgentStatTiles', () => {
  const props = {
    agents: { total: 2, healthy: 2, provisioning: 0, delegated: 0 },
    runs: { running: 0, starting: 0, queued: 0, longestMs: 0 },
    outcomes: { completed: 3, failed: 1 },
    periodText: 'last 7 days',
    outcomesText: 'last 7 days',
  };

  it('shows a dash and the note for tokens that are not loaded', () => {
    renderWithProviders(<AgentStatTiles {...props} tokens={null} tokensNote="Loading" />);
    expect(screen.getByText('–')).toBeInTheDocument();
    expect(screen.getByText('Loading')).toBeInTheDocument();
  });

  it('shows the figures once they are loaded', () => {
    renderWithProviders(
      <AgentStatTiles {...props} tokens={{ cached: 0, input: 1500, output: 500 }} tokensNote="" />,
    );
    expect(screen.queryByText('–')).not.toBeInTheDocument();
    expect(screen.getByText('2.0K')).toBeInTheDocument();
  });
});
