import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { Text } from '@runkora/hades';
import {
  BudgetLimitFields,
  EMPTY_BUDGET_LIMIT_VALUES,
  budgetFromLimitValues,
  budgetLimitProblem,
  budgetLimitValuesFrom,
  type BudgetLimitMode,
  type BudgetLimitValues,
} from './budget-limit-fields';

const values = (patch: Partial<BudgetLimitValues>): BudgetLimitValues => ({
  ...EMPTY_BUDGET_LIMIT_VALUES,
  ...patch,
});

function Harness({ mode, initial }: { mode: BudgetLimitMode; initial?: BudgetLimitValues }) {
  const [v, setV] = useState(initial ?? EMPTY_BUDGET_LIMIT_VALUES);
  return (
    <>
      <BudgetLimitFields mode={mode} values={v} onChange={(p) => setV((c) => ({ ...c, ...p }))} />
      <Text data-testid="budget">{JSON.stringify(budgetFromLimitValues(v, mode))}</Text>
    </>
  );
}

const budget = () => JSON.parse(screen.getByTestId('budget').textContent!);

describe('BudgetLimitFields', () => {
  it('renders every control whether a row is on or off, so toggling moves nothing', () => {
    render(<Harness mode="grant" />);
    const before = screen.getAllByRole('textbox').length;
    expect(screen.getByLabelText('Spend limit in USD')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Weekly' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('switch', { name: 'Spend limit' }));
    fireEvent.click(screen.getByRole('switch', { name: 'Per-run limit' }));
    expect(screen.getAllByRole('textbox')).toHaveLength(before);
  });

  it('dims a row while it is off and restores it when it is on', () => {
    render(<Harness mode="grant" />);
    const dimmed = () =>
      screen.getByLabelText('Spend limit in USD').closest('[class*="opacity-50"]');
    expect(dimmed()).not.toBeNull();
    fireEvent.click(screen.getByRole('switch', { name: 'Spend limit' }));
    expect(dimmed()).toBeNull();
  });

  it('has no per-run row in role mode and offers the shared pool', () => {
    render(<Harness mode="role" />);
    expect(screen.queryByRole('switch', { name: 'Per-run limit' })).toBeNull();
    expect(screen.getByRole('switch', { name: 'Shared pool' })).toBeInTheDocument();
  });

  it('has no shared pool in grant mode', () => {
    render(<Harness mode="grant" />);
    expect(screen.queryByRole('switch', { name: 'Shared pool' })).toBeNull();
  });

  it('describes the per-run limit as capping a run started on the person behalf', () => {
    render(<Harness mode="grant" />);
    expect(
      screen.getByText(/single agent run started on this person's behalf/),
    ).toBeInTheDocument();
  });

  it('flags a typed amount that is not above 0 on its own row', () => {
    render(<Harness mode="grant" />);
    fireEvent.click(screen.getByRole('switch', { name: 'Per-run limit' }));
    fireEvent.change(screen.getByLabelText('Per-run limit in USD'), { target: { value: '-1' } });
    expect(screen.getByRole('alert')).toHaveTextContent('Enter an amount above 0.');
  });

  it.each([
    ['spend only', '25', '', { limit_usd: 25 }],
    ['per-run only', '', '4', { per_run_limit_usd: 4 }],
    ['both', '25', '4', { limit_usd: 25, per_run_limit_usd: 4 }],
  ])('maps %s to a payload', (_n, spend, perRun, expected) => {
    render(<Harness mode="grant" />);
    if (spend) {
      fireEvent.click(screen.getByRole('switch', { name: 'Spend limit' }));
      fireEvent.change(screen.getByLabelText('Spend limit in USD'), { target: { value: spend } });
    }
    if (perRun) {
      fireEvent.click(screen.getByRole('switch', { name: 'Per-run limit' }));
      fireEvent.change(screen.getByLabelText('Per-run limit in USD'), {
        target: { value: perRun },
      });
    }
    expect(budget()).toEqual({ reset_cadence: 'monthly', ...expected });
  });

  it('maps no rows to no budget and carries the weekly cadence', () => {
    render(<Harness mode="grant" />);
    expect(budget()).toBeNull();
    fireEvent.click(screen.getByRole('switch', { name: 'Spend limit' }));
    fireEvent.change(screen.getByLabelText('Spend limit in USD'), { target: { value: '9' } });
    fireEvent.click(screen.getByRole('button', { name: 'Weekly' }));
    expect(budget()).toEqual({ reset_cadence: 'weekly', limit_usd: 9 });
  });
});

describe('budgetLimitValuesFrom', () => {
  it('turns on the rows the budget has', () => {
    expect(
      budgetLimitValuesFrom({ limit_usd: 50, reset_cadence: 'weekly', per_run_limit_usd: 5 }),
    ).toEqual(
      values({ spendOn: true, spendUsd: '50', cadence: 'weekly', perRunOn: true, perRunUsd: '5' }),
    );
  });

  it('leaves absent limits off', () => {
    expect(budgetLimitValuesFrom({ per_run_limit_usd: 3, reset_cadence: 'monthly' })).toEqual(
      values({ perRunOn: true, perRunUsd: '3' }),
    );
    expect(budgetLimitValuesFrom(null)).toEqual(EMPTY_BUDGET_LIMIT_VALUES);
    expect(budgetLimitValuesFrom(undefined)).toEqual(EMPTY_BUDGET_LIMIT_VALUES);
  });

  it('carries a role pool', () => {
    expect(
      budgetLimitValuesFrom({ limit_usd: 9, reset_cadence: 'monthly', pooled: true }).pooled,
    ).toBe(true);
  });
});

describe('budgetLimitProblem', () => {
  it.each([
    ['grant, nothing on', 'grant', {}, 'Turn on a spend limit or a per-run limit.'],
    ['role, spend off', 'role', { perRunOn: true }, 'Turn on the spend limit.'],
    ['spend on, blank', 'grant', { spendOn: true }, 'Enter a spend limit above 0.'],
    ['spend on, zero', 'grant', { spendOn: true, spendUsd: '0' }, 'Enter a spend limit above 0.'],
    ['spend on, text', 'grant', { spendOn: true, spendUsd: 'abc' }, 'Enter a spend limit above 0.'],
    [
      'per-run on, negative',
      'grant',
      { perRunOn: true, perRunUsd: '-2' },
      'Enter a per-run limit above 0.',
    ],
  ] as const)('%s', (_n, mode, patch, message) => {
    expect(budgetLimitProblem(values(patch), mode)).toBe(message);
  });

  it('accepts a valid grant and role', () => {
    expect(budgetLimitProblem(values({ perRunOn: true, perRunUsd: '2' }), 'grant')).toBeNull();
    expect(budgetLimitProblem(values({ spendOn: true, spendUsd: '2' }), 'role')).toBeNull();
  });

  it('ignores a per-run row that is on in role mode', () => {
    expect(
      budgetFromLimitValues(
        values({ spendOn: true, spendUsd: '2', perRunOn: true, perRunUsd: '9' }),
        'role',
      ),
    ).toEqual({ reset_cadence: 'monthly', limit_usd: 2 });
  });
});
