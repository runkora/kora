import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { GuardrailsFields, type GuardrailValues } from './guardrail-rows';
import { loopGuardrailValues } from '../../_types';

const VALUES: GuardrailValues = {
  agentBudgetOn: false,
  agentBudgetUsd: '500.00',
  agentBudgetCadence: 'monthly',
  runBudgetOn: false,
  runBudgetUsd: '5.00',
  capacityOn: false,
  capacity: '10',
  ...loopGuardrailValues(undefined),
};

// Each input renders twice (wide and narrow layouts); the first is enough.
const field = (name: string) => screen.getAllByLabelText(name)[0];

describe('GuardrailsFields loop thresholds', () => {
  it('starts off, showing the platform defaults', () => {
    render(<GuardrailsFields values={VALUES} onChange={vi.fn()} />);
    expect(screen.getByRole('switch', { name: 'Repeated calls' })).toHaveAttribute(
      'aria-checked',
      'false',
    );
    expect(field('Repeated calls: warn after')).toHaveValue('4');
    expect(field('Repeated calls: pause after')).toHaveValue('8');
    expect(field('Wait loops: warn after')).toHaveValue('6');
    expect(field('Wait loops: pause after')).toHaveValue('12');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('switching a pair off restores its defaults', () => {
    const onChange = vi.fn();
    render(
      <GuardrailsFields
        values={{ ...VALUES, waitRearmOn: true, waitRearmWarn: '2', waitRearmPause: '40' }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole('switch', { name: 'Wait loops' }));
    expect(onChange).toHaveBeenCalledWith('waitRearmOn', false);
    expect(onChange).toHaveBeenCalledWith('waitRearmWarn', '6');
    expect(onChange).toHaveBeenCalledWith('waitRearmPause', '12');
  });

  it('reports edits as free numbers', () => {
    const onChange = vi.fn();
    render(<GuardrailsFields values={{ ...VALUES, waitRearmOn: true }} onChange={onChange} />);
    fireEvent.change(field('Wait loops: pause after'), { target: { value: '40' } });
    expect(onChange).toHaveBeenCalledWith('waitRearmPause', '40');
  });

  it('flags a pause that is not above its warn, as the API does', () => {
    render(
      <GuardrailsFields
        values={{ ...VALUES, repeatedCallOn: true, repeatedCallWarn: '5', repeatedCallPause: '5' }}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Pause after must be greater than warn after.',
    );
  });
});
