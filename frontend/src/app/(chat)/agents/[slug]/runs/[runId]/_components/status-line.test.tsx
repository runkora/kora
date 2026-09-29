import { describe, expect, it, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { StatusLine, statusText } from './status-line';
import { HumanTurnItem } from './human-turn';

describe('statusText', () => {
  const now = 100_000;
  it.each([
    [{ kind: 'sandbox', since: now - 24_000 } as const, 'Starting sandbox… 24s'],
    [
      { kind: 'retry', attempt: 3, maxRetries: 10, error: 'authentication_failed' } as const,
      'Model provider error (authentication failed), retrying (3/10)',
    ],
    [{ kind: 'thinking', since: now - 90_000 } as const, 'Thinking… 1m 30s'],
    [{ kind: 'thinking', since: now - 1_000 } as const, null],
    [
      {
        kind: 'tool',
        since: now - 5_000,
        tool: {
          id: 't',
          name: 'Bash',
          input: { command: 'go test ./...' },
          status: 'running',
          result: '',
          progressSeconds: 40,
        },
      } as const,
      'Ran go test ./... · 40s',
    ],
  ])('%#', (activity, want) => {
    expect(statusText(activity, now)).toBe(want);
  });
});

describe('HumanTurnItem', () => {
  it('shows delivery state and a failed send', () => {
    const { rerender } = render(
      <HumanTurnItem
        turn={{ type: 'human', id: 'h', content: 'hi', timestamp: '', delivery: 'queued' }}
      />,
    );
    expect(screen.getByText(/Queued/)).toBeInTheDocument();
    rerender(
      <HumanTurnItem
        turn={{
          type: 'human',
          id: 'h',
          content: 'hi',
          timestamp: '',
          delivery: 'failed',
          error: 'Run ended.',
        }}
      />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Not sent. Run ended.');
  });
});

describe('StatusLine', () => {
  it('counts up while the phase lasts', () => {
    vi.useFakeTimers();
    try {
      render(<StatusLine activity={{ kind: 'sandbox', since: Date.now() - 3000 }} />);
      expect(screen.getByText('Starting sandbox… 3s')).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByText('Starting sandbox… 8s')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});
