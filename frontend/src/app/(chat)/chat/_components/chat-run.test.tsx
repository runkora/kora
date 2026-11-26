import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { RunWithAttribution } from '@runkora/shared/api';
import { handedOver, handOver } from '../_lib/hand-over';
import { ChatRunRoute } from './chat-run';

const own = vi.hoisted(() => ({ value: { runs: [], error: null, loaded: false } }));
vi.mock('../_hooks/use-own-runs', () => ({ useOwnRuns: () => own.value }));
vi.mock('@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-page', async () => {
  const { Text } = await import('@runkora/hades');
  return {
    RunPage: ({ enter, pane }: { enter?: { layout: string }; pane: (p: object) => unknown }) => (
      <>
        <Text>{`entered from ${enter?.layout ?? 'nowhere'}`}</Text>
        {pane({ run: {}, rows: [] }) as React.ReactNode}
      </>
    ),
  };
});
vi.mock('./history-pane', async () => {
  const { Text } = await import('@runkora/hades');
  return {
    HistoryPane: ({ own: o }: { own: { runs: RunWithAttribution[] } }) => (
      <Text>{`${o.runs.length} runs`}</Text>
    ),
  };
});

describe('ChatRunRoute', () => {
  it('takes the composer box and the runs the chat page handed over, once', () => {
    const runs = [{ id: 'r1' }] as RunWithAttribution[];
    handOver('run-9', {
      own: { runs, error: null, loaded: true },
      enter: { from: new DOMRect(), layout: 'laptop' },
    });
    render(<ChatRunRoute runId="run-9" />);
    expect(screen.getByText('entered from laptop')).toBeInTheDocument();
    expect(screen.getByText('1 runs')).toBeInTheDocument();
    expect(handedOver('run-9')).toBeUndefined();
  });

  it('opens a reloaded run with no move and its own read of the runs', () => {
    render(<ChatRunRoute runId="run-1" />);
    expect(screen.getByText('entered from nowhere')).toBeInTheDocument();
    expect(screen.getByText('0 runs')).toBeInTheDocument();
  });
});
