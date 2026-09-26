import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { StartedByCell } from './started-by-cell';
import type { RunRow } from '../../_types';

function run(overrides: Partial<RunRow> = {}): RunRow {
  return {
    id: 'run-1',
    agentTypeId: 'at-1',
    agentTypeName: 'Coder',
    agentTypeSlug: 'coder',
    state: 'completed',
    createdBy: 'trigger-source:cd940135-0621-437f-bce2-95d021f18cb6',
    createdByName: null,
    startedByTrigger: null,
    delegated: false,
    startedAt: '2026-09-01T00:00:00Z',
    completedAt: '2026-09-01T00:01:00Z',
    createdAt: '2026-09-01T00:00:00Z',
    durationMs: 60_000,
    tokensUsed: 100,
    toolCalls: 1,
    summary: null,
    ...overrides,
  };
}

describe('StartedByCell', () => {
  it('shows the trigger name, not the raw source principal, for a trigger-started run', () => {
    render(
      <StartedByCell
        row={run({
          startedByTrigger: {
            triggerId: 'trg-1',
            triggerName: 'Constellation IaC Code Review',
            deliveryId: 'del-1',
          },
        })}
      />,
    );
    expect(screen.getByText('Constellation IaC Code Review')).toBeInTheDocument();
    expect(screen.queryByText(/trigger-source:/)).not.toBeInTheDocument();
  });

  it('falls back to the trigger id when the trigger has no name', () => {
    render(
      <StartedByCell
        row={run({
          startedByTrigger: { triggerId: 'trg-1', triggerName: null, deliveryId: null },
        })}
      />,
    );
    expect(screen.getByText('trg-1')).toBeInTheDocument();
  });

  it('shows the creator when no trigger started the run', () => {
    render(<StartedByCell row={run({ createdByName: 'Jane Doe' })} />);
    expect(screen.getByText('Jane Doe')).toBeInTheDocument();
  });

  it('shows the Delegated badge by default when the run is delegated', () => {
    render(<StartedByCell row={run({ delegated: true })} />);
    expect(screen.getByText('Delegated')).toBeInTheDocument();
  });

  it('hides the Delegated badge when asked to', () => {
    render(<StartedByCell row={run({ delegated: true })} showDelegatedBadge={false} />);
    expect(screen.queryByText('Delegated')).not.toBeInTheDocument();
  });
});
