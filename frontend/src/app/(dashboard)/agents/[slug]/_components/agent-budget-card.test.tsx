import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { AgentBudgetCard } from './agent-budget-card';

const win = (limit: number, spent: number) => ({
  cadence: 'monthly' as const,
  limit,
  spent,
  resetsAt: '2026-11-01T00:00:00Z',
});

describe('AgentBudgetCard', () => {
  it('shows each window and the per-run limit', () => {
    renderWithProviders(
      <AgentBudgetCard budget={{ name: 'Coder', perRunLimit: 4, windows: [win(100, 40)] }} />,
    );
    expect(screen.getByText('Monthly budget')).toBeInTheDocument();
    expect(screen.getByText('$4.00 / run')).toBeInTheDocument();
    expect(screen.queryByText(/ceiling reached/)).not.toBeInTheDocument();
  });

  it('flags a window that is fully spent', () => {
    renderWithProviders(<AgentBudgetCard budget={{ name: 'Coder', windows: [win(10, 10)] }} />);
    expect(screen.getByText(/Budget ceiling reached/)).toBeInTheDocument();
  });

  it('says so when there is no budget', () => {
    renderWithProviders(<AgentBudgetCard budget={null} />);
    expect(screen.getByText(/No budget configured/)).toBeInTheDocument();
  });
});
