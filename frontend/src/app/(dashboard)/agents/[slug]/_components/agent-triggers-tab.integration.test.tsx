/**
 * The agent tab resolves a run's agent slug from the triggers it loaded. Every
 * trigger on this tab belongs to the agent whose page it is, so the resolver
 * looks the trigger up rather than assuming. If it is dropped, the run id in
 * the detail panel silently degrades to plain text on this surface only, while
 * still working on the platform Triggers page: exactly the kind of difference
 * nobody notices until someone cannot click through from an agent.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';

vi.mock('next/navigation', () => ({
  useRouter: vi.fn().mockReturnValue({ refresh: vi.fn(), push: vi.fn() }),
  usePathname: vi.fn().mockReturnValue('/agents/coder'),
  useSearchParams: vi.fn().mockReturnValue(new URLSearchParams()),
}));

vi.mock('../../../triggers/_services/triggers-api', () => ({
  listSources: vi.fn(),
  listTriggers: vi.fn(),
  listDeliveries: vi.fn(),
  redeliverDelivery: vi.fn(),
}));

vi.mock('../_context/agent-detail-context', () => ({
  useAgentDetailContext: vi.fn().mockReturnValue({
    agent: { id: 'a1', slug: 'coder', name: 'Coder', identityMode: 'direct' },
  }),
}));

import {
  listSources,
  listTriggers,
  listDeliveries,
} from '../../../triggers/_services/triggers-api';
import { AgentTriggersTab } from './agent-triggers-tab';

const TRIGGER = {
  id: 't1',
  name: 'gh-push',
  kind: 'webhook',
  agent_slug: 'coder',
  agent_type_id: 'a1',
  enabled: true,
  created_at: '2026-07-28T09:00:00Z',
  updated_at: '2026-07-28T09:00:00Z',
};

const DELIVERY = {
  id: 'd-1',
  kind: 'webhook',
  source_id: 'src-1',
  event_type: 'push',
  status: 'dispatched',
  received_at: '2026-07-28T09:00:00Z',
  trigger_id: 't1',
  run_id: 'run-7',
  runs: [
    {
      trigger_id: 't1',
      trigger_name: 'gh-push',
      run_id: 'run-7',
      status: 'dispatched',
      error: null,
      created_at: '2026-07-28T09:00:01Z',
    },
  ],
};

beforeEach(() => {
  vi.mocked(listSources).mockResolvedValue({ sources: [], error: null } as never);
  vi.mocked(listTriggers).mockResolvedValue({ triggers: [TRIGGER], error: null } as never);
  vi.mocked(listDeliveries).mockResolvedValue({
    deliveries: [DELIVERY],
    nextCursor: null,
    error: null,
  } as never);
});

describe('AgentTriggersTab delivery detail', () => {
  it('resolves the agent slug so the run links from this tab too', async () => {
    renderWithProviders(<AgentTriggersTab />);

    const row = await screen.findByRole('row', { name: /View payload and outcome/ });
    await userEvent.click(row);

    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'run-7' })).toHaveAttribute(
        'href',
        '/agents/coder/runs/run-7',
      ),
    );
  });
});
