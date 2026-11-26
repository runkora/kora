import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { RunWithAttribution } from '@runkora/shared/api';
import type { RunView } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-view';
import { HistoryPane } from './history-pane';

const run = (id: string, summary: string, agent_slug?: string) =>
  ({
    id,
    summary,
    agent_slug,
    state: 'completed',
    created_at: new Date().toISOString(),
  }) as RunWithAttribution;

const own = (runs: RunWithAttribution[]) => ({ runs, error: null, loaded: true });

const open = {
  run: { id: 'run-open', summary: 'Add retries to the relay', durationMs: 0 } as RunView,
  rows: [],
};

describe('HistoryPane', () => {
  it('opens each of the viewer’s runs on the run page with from=chat', () => {
    render(<HistoryPane own={own([run('run-1', 'Fix the flaky test', 'coder')])} />);
    expect(screen.getByRole('link', { name: /Fix the flaky test/ })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-1?from=chat',
    );
    expect(screen.getByRole('link', { name: 'Home' })).toHaveAttribute('href', '/');
  });

  it('shows the open run first and links New run to an empty chat page', () => {
    render(
      <HistoryPane
        own={own([run('run-open', 'Old title', 'coder'), run('run-1', 'Earlier', 'coder')])}
        open={open}
      />,
    );
    const labels = screen.getAllByText(/Add retries to the relay|Earlier|Old title/);
    expect(labels.map((l) => l.textContent)).toEqual(['Add retries to the relay', 'Earlier']);
    expect(screen.getByRole('link', { name: /New run/ })).toHaveAttribute('href', '/chat');
  });

  it('does not link New run on the empty chat page, and says where runs will appear', () => {
    render(<HistoryPane own={own([])} />);
    expect(screen.queryByRole('link', { name: /New run/ })).toBeNull();
    expect(screen.getByText('Runs you start appear here.')).toBeInTheDocument();
  });

  it('leaves a run without an agent slug unlinked', () => {
    render(<HistoryPane own={own([run('run-1', 'Orphan')])} />);
    expect(screen.queryByRole('link', { name: /Orphan/ })).toBeNull();
  });
});
