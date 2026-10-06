import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { APP_ROUTES } from '@orbit/lib/routing';
import { toAgentTypeRow, toRunRow, indexAgentTypes } from '../../_types';
import { CODE_REVIEWER, RAW_RUNS } from '../../_components/agents-mock-data';
import { AgentDetailProvider } from '../_context/agent-detail-context';
import { RecentRunsTable } from './agent-overview-cards';

const push = vi.fn();
vi.mock('next/navigation', async (orig) => ({
  ...(await orig<typeof import('next/navigation')>()),
  useRouter: () => ({ push }),
}));

const agent = toAgentTypeRow(CODE_REVIEWER);

describe('RecentRunsTable', () => {
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

  it('shows the five newest runs and opens a run on click', async () => {
    renderWithProviders(
      <AgentDetailProvider
        initialAgent={agent}
        initialRuns={rows}
        initialError={null}
        initialStatus="success"
      >
        <RecentRunsTable />
      </AgentDetailProvider>,
    );
    const shown = screen.getAllByText(/^run-\d-xx$/).map((el) => el.textContent);
    expect(shown).toEqual(['run-6-xx', 'run-5-xx', 'run-4-xx', 'run-3-xx', 'run-2-xx']);

    await userEvent.click(screen.getByText('run-6-xx'));
    expect(push).toHaveBeenCalledWith(APP_ROUTES.agents.run(agent.slug, 'run-6-xxxxxxxx'));
  });
});
