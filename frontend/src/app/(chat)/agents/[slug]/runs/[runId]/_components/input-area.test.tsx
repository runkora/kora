import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders as render } from '@runkora/test-utils';
import type { UseRunReturn } from '../_hooks/use-run';
import { InputArea } from './input-area';

let ctx: Pick<
  UseRunReturn,
  | 'agentName'
  | 'runState'
  | 'isStarting'
  | 'addHumanMessage'
  | 'interruptRun'
  | 'canSteer'
  | 'delegated'
  | 'stopError'
>;
vi.mock('../_context/run-context', () => ({ useRunContext: () => ctx }));

beforeEach(() => {
  ctx = {
    agentName: 'Reviewer',
    runState: 'active',
    isStarting: false,
    addHumanMessage: vi.fn(),
    interruptRun: vi.fn(),
    canSteer: true,
    delegated: false,
    stopError: null,
  };
});

describe('InputArea', () => {
  it.each([
    ['pending', 'Reviewer is queued…'],
    ['starting', 'Reviewer is starting…'],
  ] as const)('blocks messages while the run is %s', (runState, placeholder) => {
    ctx = { ...ctx, runState, isStarting: true };
    render(<InputArea />);
    expect(screen.getByPlaceholderText(placeholder)).toBeDisabled();
  });

  it('accepts messages once the run is active', () => {
    render(<InputArea />);
    expect(
      screen.getByPlaceholderText('Message Reviewer. It reads this at its next step.'),
    ).toBeEnabled();
  });

  it.each([
    [false, "You don't have permission to message this run."],
    [true, 'Only the person who started this run can message it.'],
  ] as const)(
    'blocks messages and Stop when the caller cannot steer (delegated %s)',
    (delegated, placeholder) => {
      ctx = { ...ctx, canSteer: false, delegated };
      render(<InputArea />);
      expect(screen.getByPlaceholderText(placeholder)).toBeDisabled();
      expect(screen.getByLabelText('Stop agent')).toBeDisabled();
      expect(
        screen.getByLabelText("You don't have permission to stop this run."),
      ).toBeInTheDocument();
    },
  );

  it('leaves the composer to the API until access is known', () => {
    ctx = { ...ctx, canSteer: null };
    render(<InputArea />);
    expect(
      screen.getByPlaceholderText('Message Reviewer. It reads this at its next step.'),
    ).toBeEnabled();
  });

  it('shows why a Stop was refused', () => {
    ctx = { ...ctx, stopError: 'You cannot stop this run.' };
    render(<InputArea />);
    expect(screen.getByRole('alert')).toHaveTextContent('You cannot stop this run.');
  });
});
