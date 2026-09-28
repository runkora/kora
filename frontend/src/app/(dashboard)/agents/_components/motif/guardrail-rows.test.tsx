import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { GuardrailsFields, type GuardrailValues } from './guardrail-rows';

const VALUES: GuardrailValues = {
  agentBudgetOn: false,
  agentBudgetUsd: '500.00',
  agentBudgetCadence: 'monthly',
  runBudgetOn: false,
  runBudgetUsd: '5.00',
  capacityOn: false,
  capacity: '10',
  subagentCapOn: true,
  subagentCap: '1',
  nestedSubagents: true,
};

describe('GuardrailsFields subagent policy', () => {
  it('shows the subagent cap and the nesting switch', () => {
    render(<GuardrailsFields values={VALUES} onChange={vi.fn()} />);
    expect(screen.getAllByLabelText('Max concurrent subagents')[0]).toHaveValue('1');
    expect(screen.getByRole('switch', { name: 'Subagents' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('switch', { name: 'Nested subagents' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
  });

  it('reports cap edits and the nesting switch', () => {
    const onChange = vi.fn();
    render(<GuardrailsFields values={VALUES} onChange={onChange} />);
    fireEvent.change(screen.getAllByLabelText('Max concurrent subagents')[0], {
      target: { value: '3' },
    });
    expect(onChange).toHaveBeenCalledWith('subagentCap', '3');
    fireEvent.click(screen.getByRole('switch', { name: 'Nested subagents' }));
    expect(onChange).toHaveBeenCalledWith('nestedSubagents', false);
  });
});
