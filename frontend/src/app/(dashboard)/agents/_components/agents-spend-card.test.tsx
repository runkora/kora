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

const win = (cadence: 'weekly' | 'monthly', limit: number, spent: number) => ({
  cadence,
  limit,
  spent,
  resetsAt: '2026-11-01T00:00:00Z',
});

describe('AgentsSpendCard', () => {
  it('shows a bar for each budget window, weekly ones as a week', async () => {
    renderWithProviders(
      <AgentsSpendCard
        {...BASE}
        budgets={[
          { name: 'Coder', windows: [win('monthly', 100, 40)] },
          { name: 'Triage', windows: [win('weekly', 20, 3), win('monthly', 80, 30)] },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));

    expect(screen.getByText('of $100.00')).toBeInTheDocument();
    expect(screen.getByText('of $20.00 a week')).toBeInTheDocument();
    expect(screen.getByText('of $80.00')).toBeInTheDocument();
    expect(screen.getByText('Triage (weekly)')).toBeInTheDocument();
    expect(screen.getByText('Triage (monthly)')).toBeInTheDocument();
    expect(screen.getByText(/Spend is counted by agent/)).toBeInTheDocument();
  });

  it('shows an agent with a limit and no spend as an empty bar', async () => {
    renderWithProviders(
      <AgentsSpendCard {...BASE} budgets={[{ name: 'Idle', windows: [win('monthly', 40, 0)] }]} />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));
    expect(screen.getByText('Idle')).toBeInTheDocument();
    expect(screen.getByText('of $40.00')).toBeInTheDocument();
  });

  it('lists spend of deleted agents as a row, so the rows add up to the total', () => {
    renderWithProviders(<AgentsSpendCard {...BASE} total={16.5} deletedCost={6.5} />);
    expect(screen.getByText('Deleted agents')).toBeInTheDocument();
    expect(screen.getByText('$6.50')).toBeInTheDocument();
  });

  it('shows deleted spend even when no current agent has spent', () => {
    renderWithProviders(<AgentsSpendCard {...BASE} agents={[]} total={6.5} deletedCost={6.5} />);
    expect(screen.getByText('Deleted agents')).toBeInTheDocument();
    expect(screen.queryByText('No spend in this window yet.')).not.toBeInTheDocument();
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

describe('AgentsSpendCard budgets', () => {
  it('shows a zero limit as a limit that is spent', async () => {
    renderWithProviders(
      <AgentsSpendCard {...BASE} budgets={[{ name: 'Capped', windows: [win('monthly', 0, 2)] }]} />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Budgets' }));
    expect(screen.getByText('of $0.00')).toBeInTheDocument();
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
    const budgets = agents.map((a, i) => ({
      name: a.name,
      windows: [win('monthly', 1000 - i, i)],
    }));
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
