import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import type { RunView } from '../_lib/run-view';
import { RunHeader } from './run-header';

vi.mock('../_lib/run-layout', () => ({ useRunLayout: () => 'desktop' }));

const view = (over: Partial<RunView> = {}): RunView =>
  ({
    id: 'run-1',
    summary: 'Review a pull request',
    agent: { name: 'Code reviewer', slug: 'code-reviewer' },
    harness: 'claude-code',
    model: '',
    startedAt: '2026-11-11T01:00:00Z',
    durationMs: 0,
    spend: { usd: 0 },
    sources: [],
    entries: [],
    ...over,
  }) as RunView;

const LABELS = /Starting|Working|Completed|Failed|Waiting for a reply|Budget spent/;

describe('RunHeader state label', () => {
  it('shows no state label while the state is unknown', () => {
    const { container } = render(<RunHeader run={view()} canRun={false} />);
    expect(container.textContent).not.toMatch(LABELS);
  });

  it('reads "Starting" for a pending run', () => {
    const { container } = render(<RunHeader run={view({ state: 'pending' })} canRun={false} />);
    expect(container.textContent).toContain('Starting');
  });
});
