import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { UseRunReturn } from '../_hooks/use-run';
import { InputArea } from './input-area';

let ctx: Pick<
  UseRunReturn,
  'agentName' | 'runState' | 'isStarting' | 'addHumanMessage' | 'interruptRun'
>;
vi.mock('../_context/run-context', () => ({ useRunContext: () => ctx }));

beforeEach(() => {
  ctx = {
    agentName: 'Reviewer',
    runState: 'active',
    isStarting: false,
    addHumanMessage: vi.fn(),
    interruptRun: vi.fn(),
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
});
