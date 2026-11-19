/**
 * Integration tests for GET /api/agents/runs/{runId}/grades. MSW stands in for kora-agents.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraAgentsUrl: 'http://test-agents' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@runkora/server/session';

const get = (runId: string) =>
  GET(new NextRequest(`http://localhost/api/agents/runs/${runId}/grades`), {
    params: Promise.resolve({ runId }),
  } as never);

const GRADE = {
  grader: 'review-outcome',
  description: 'Posts a review.',
  version: '4c1e9a2',
  model: 'jev-1-14',
  status: 'graded',
  verdict: 'pass',
  applies_p: null,
  state_cap: null,
  input_tokens: 24180,
  model_served: 'jev-1-14',
  cost_usd: 0.001,
  graded_at: '2026-11-18T00:00:00Z',
  answers: [],
};

describe('GET /api/agents/runs/{runId}/grades', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it("returns the run's grades with the caller's token", async () => {
    let auth: string | null = null;
    server.use(
      http.get('http://test-agents/api/v1/runs/run-1/grades', ({ request }) => {
        auth = request.headers.get('Authorization');
        return HttpResponse.json({ grades: [GRADE] });
      }),
    );

    const res = await get('run-1');

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ grades: [GRADE] });
    expect(auth).toBe('Bearer test-access-token');
  });

  it('passes a 404 through with its status', async () => {
    server.use(
      http.get('http://test-agents/api/v1/runs/run-2/grades', () =>
        HttpResponse.json(
          { code: 'resource_not_found', message: 'run not found' },
          { status: 404 },
        ),
      ),
    );

    const res = await get('run-2');

    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ code: 'resource_not_found', message: 'run not found' });
  });

  it('returns 401 without a valid session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await get('run-1');
    expect(res.status).toBe(401);
  });
});
