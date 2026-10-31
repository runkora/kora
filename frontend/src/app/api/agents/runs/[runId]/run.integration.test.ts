/**
 * Integration tests for GET /api/agents/runs/[runId]: the trace link built from
 * the chart's trace URL template and the run's trace_id.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraAgentsUrl: 'http://test-agents-svc' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

vi.mock('@runkora/server/services/identity-server', () => ({
  resolveUserDisplayNames: vi.fn().mockResolvedValue(new Map()),
}));

import { GET } from './route';
import { getSession } from '@runkora/server/session';

function runWith(traceId: string | null) {
  server.use(
    http.get('http://test-agents-svc/api/v1/runs/run-1', () =>
      HttpResponse.json({
        id: 'run-1',
        agent_type_id: 'at-1',
        created_by: 'u-1',
        trace_id: traceId,
      }),
    ),
    http.get('http://test-agents-svc/api/v1/agent-types/at-1', () =>
      HttpResponse.json({ id: 'at-1', name: 'Agent' }),
    ),
  );
}

async function traceUrl(): Promise<unknown> {
  const res = await GET(new NextRequest('http://localhost/api/agents/runs/run-1'), {
    params: Promise.resolve({ runId: 'run-1' }),
  } as never);
  return ((await res.json()) as { trace_url?: unknown }).trace_url;
}

describe('GET /api/agents/runs/[runId]', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      user: { sub: 'u-1' },
      accessToken: 'test-access-token',
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    } as never);
  });
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it('fills the trace URL template with the run trace_id', async () => {
    vi.stubEnv('KORA_TRACE_URL_TEMPLATE', 'https://grafana.test/explore?trace={trace_id}');
    runWith('4bf92f3577b34da6a3ce929d0e0e4736');
    expect(await traceUrl()).toBe(
      'https://grafana.test/explore?trace=4bf92f3577b34da6a3ce929d0e0e4736',
    );
  });

  it('returns no link without a template', async () => {
    vi.stubEnv('KORA_TRACE_URL_TEMPLATE', '');
    runWith('4bf92f3577b34da6a3ce929d0e0e4736');
    expect(await traceUrl()).toBeNull();
  });

  it('returns no link for an unsampled run', async () => {
    vi.stubEnv('KORA_TRACE_URL_TEMPLATE', 'https://grafana.test/explore?trace={trace_id}');
    runWith(null);
    expect(await traceUrl()).toBeNull();
  });
});
