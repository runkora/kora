import { describe, expect, it, vi } from 'vitest';
import { renderWithProviders } from '@runkora/test-utils';
import { toAgentTypeRow } from '../../_types';
import { CODE_REVIEWER } from '../../_components/agents-mock-data';
import { toAgentOverview } from '../../_lib/overview';
import { AgentDetailProvider } from '../_context/agent-detail-context';
import { mockOverview, mockRunStatsOnly } from './agent-overview-mock-data';
import { AgentOverviewTab } from './agent-overview-tab';

vi.mock('next/navigation', async (orig) => ({
  ...(await orig<typeof import('next/navigation')>()),
  useRouter: () => ({ push: vi.fn() }),
}));

const agent = toAgentTypeRow(CODE_REVIEWER);

function statGrid(resp: ReturnType<typeof mockOverview>) {
  const { container } = renderWithProviders(
    <AgentDetailProvider
      initialAgent={agent}
      initialRuns={[]}
      initialNextCursor={null}
      initialStateCounts={null}
      initialError={null}
      initialStatus="success"
    >
      <AgentOverviewTab
        overview={toAgentOverview(resp)}
        status="ready"
        onEditInstructions={() => {}}
      />
    </AgentDetailProvider>,
  );
  return container.querySelector('.grid') as HTMLElement;
}

describe('AgentOverviewTab stat row', () => {
  it('uses five columns with runs and usage', () => {
    expect(statGrid(mockOverview(agent.slug, '7d')).className).toContain('xl:grid-cols-5');
  });

  it('shares the full width between the run tiles without usage', () => {
    const grid = statGrid(mockRunStatsOnly(agent.slug, '7d'));
    expect(grid.className).toContain('xl:grid-cols-3');
    expect(grid.className).not.toContain('xl:grid-cols-5');
  });
});
