import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@runkora/hades';
import {
  BudgetLimitFields,
  budgetLimitProblem,
  budgetLimitValuesFrom,
  type BudgetLimitMode,
  type BudgetLimits,
} from './budget-limit-fields';

/**
 * The limits of a budget as switchable rows, laid out like the agent builder's Guardrails. Each row
 * has a toggle, an icon, a title and one line of help, and its controls on the right. A row that is
 * off stays rendered and dimmed, so switching it never moves the page.
 */
const meta: Meta<typeof BudgetLimitFields> = {
  title: 'App/Molecules/BudgetLimitFields',
  component: BudgetLimitFields,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof BudgetLimitFields>;

function Demo({ mode, initial }: { mode: BudgetLimitMode; initial?: BudgetLimits }) {
  const [values, setValues] = useState(() => budgetLimitValuesFrom(initial));
  const problem = budgetLimitProblem(values, mode);
  return (
    <Stack maxWidth="prose" gap="cozy">
      <BudgetLimitFields
        mode={mode}
        values={values}
        onChange={(patch) => setValues((v) => ({ ...v, ...patch }))}
      />
      <Text size="caption" color="muted">
        {problem ?? 'These values make a valid budget.'}
      </Text>
    </Stack>
  );
}

/** A grant with no budget yet: both rows are off. */
export const GrantEmpty: Story = { render: () => <Demo mode="grant" /> };

/** A grant that already has both limits: both rows are on and prefilled. */
export const GrantWithBothLimits: Story = {
  render: () => (
    <Demo mode="grant" initial={{ limit_usd: 50, reset_cadence: 'weekly', per_run_limit_usd: 5 }} />
  ),
};

/** A grant with only a per-run limit: the spend row stays off. */
export const GrantPerRunOnly: Story = {
  render: () => <Demo mode="grant" initial={{ reset_cadence: 'monthly', per_run_limit_usd: 3 }} />,
};

/** A role has a spend limit and a shared pool, and no per-run row. */
export const Role: Story = {
  render: () => (
    <Demo mode="role" initial={{ limit_usd: 200, reset_cadence: 'monthly', pooled: true }} />
  ),
};
