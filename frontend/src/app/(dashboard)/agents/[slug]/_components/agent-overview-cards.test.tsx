import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { APP_ROUTES } from '@runkora/lib/routing';
import { toAgentTypeRow, toRunRow, indexAgentTypes } from '../../_types';
import { CODE_REVIEWER, RAW_RUNS } from '../../_components/agents-mock-data';
import { AgentDetailProvider } from '../_context/agent-detail-context';
import { RecentRunsCard } from './agent-overview-cards';

const push = vi.fn();
vi.mock('next/navigation', async (orig) => ({
  ...(await orig<typeof import('next/navigation')>()),
  useRouter: () => ({ push }),
}));

const agent = toAgentTypeRow(CODE_REVIEWER);

describe('RecentRunsCard', () => {
  beforeEach(() => push.mockClear());

  const runs = Array.from({ length: 7 }, (_, i) => ({
    ...RAW_RUNS[0]!,
    id: `run-${i}-xxxxxxxx`,
    agent_type_id: CODE_REVIEWER.id,
    state: 'completed' as const,
    started_at: new Date(Date.UTC(2026, 9, 1 + i)).toISOString(),
    created_at: new Date(Date.UTC(2026, 9, 1 + i)).toISOString(),
  }));
  const rows = runs.map((r) => toRunRow(r, indexAgentTypes([agent])));

  function renderCard(onViewAll = vi.fn()) {
    return renderWithProviders(
      <AgentDetailProvider
        initialAgent={agent}
        initialRuns={rows}
        initialNextCursor={null}
        initialStateCounts={null}
        initialError={null}
        initialStatus="success"
      >
        <RecentRunsCard onViewAll={onViewAll} />
      </AgentDetailProvider>,
    );
  }

  it('shows the five newest runs and opens a run on click', async () => {
    renderCard();
    const shown = screen
      .getAllByRole('row', { name: /^Open run / })
      .map((el) => el.getAttribute('aria-label'));
    expect(shown).toEqual([
      'Open run run-6-xx',
      'Open run run-5-xx',
      'Open run run-4-xx',
      'Open run run-3-xx',
      'Open run run-2-xx',
    ]);

    await userEvent.click(screen.getByRole('row', { name: 'Open run run-6-xx' }));
    expect(push).toHaveBeenCalledWith(APP_ROUTES.agents.run(agent.slug, 'run-6-xxxxxxxx'));
  });

  it('switches to the Runs tab from View all runs', async () => {
    const onViewAll = vi.fn();
    renderCard(onViewAll);
    await userEvent.click(screen.getByRole('button', { name: /View all runs/ }));
    expect(onViewAll).toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: /Delete run/ })).not.toBeInTheDocument();
  });
});
