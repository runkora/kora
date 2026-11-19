/**
 * Integration tests for GET /api/agents/types/{typeId}/graders and
 * GET /api/agents/types/{typeId}/graders/{name}. MSW stands in for kora-agents.
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

import { GET as LIST } from './route';
import { GET as DETAIL } from './[name]/route';
import { getSession, isSessionValid } from '@runkora/server/session';

const SUMMARY = {
  name: 'review-outcome',
  description: 'Posts a review.',
  min_pass_rate: 90,
  version: '4c1e9a2',
  status: 'failing',
  passed: 20,
  graded: 30,
  too_large: 1,
};

const list = (typeId: string) =>
  LIST(new NextRequest(`http://localhost/api/agents/types/${typeId}/graders`), {
    params: Promise.resolve({ typeId }),
  } as never);

const detail = (typeId: string, name: string) =>
  DETAIL(new NextRequest(`http://localhost/api/agents/types/${typeId}/graders/${name}`), {
    params: Promise.resolve({ typeId, name }),
  } as never);

describe('GET /api/agents/types/{typeId}/graders', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it("returns the agent's Graders", async () => {
    server.use(
      http.get('http://test-agents/api/v1/agent-types/at-1/graders', () =>
        HttpResponse.json({ graders: [SUMMARY] }),
      ),
    );

    const res = await list('at-1');

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ graders: [SUMMARY] });
  });

  it('passes a 404 through with its status', async () => {
    server.use(
      http.get('http://test-agents/api/v1/agent-types/at-2/graders', () =>
        HttpResponse.json(
          { code: 'resource_not_found', message: 'agent type not found' },
          { status: 404 },
        ),
      ),
    );

    const res = await list('at-2');

    expect(res.status).toBe(404);
  });

  it('returns 401 without a valid session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    expect((await list('at-1')).status).toBe(401);
  });
});

describe('GET /api/agents/types/{typeId}/graders/{name}', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns the Grader with its questions', async () => {
    const body = {
      ...SUMMARY,
      model: 'jev-1-14',
      threshold: { pass: 0.7, fail: 0.3 },
      questions: [
        {
          name: 'read-callers',
          type: 'boolean',
          question: 'Did the agent read the callers?',
          expect: true,
          failed: 1,
          uncertain: 0,
          runs: [
            { run_id: 'run-1', title: null, started_at: '2026-11-18T00:00:00Z', verdict: 'fail' },
          ],
        },
      ],
    };
    server.use(
      http.get('http://test-agents/api/v1/agent-types/at-1/graders/review-outcome', () =>
        HttpResponse.json(body),
      ),
    );

    const res = await detail('at-1', 'review-outcome');

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(body);
  });

  it('passes a 404 through with its status', async () => {
    server.use(
      http.get('http://test-agents/api/v1/agent-types/at-1/graders/missing', () =>
        HttpResponse.json(
          { code: 'resource_not_found', message: 'grader not found' },
          { status: 404 },
        ),
      ),
    );

    const res = await detail('at-1', 'missing');

    expect(res.status).toBe(404);
    expect(await res.json()).toEqual({ code: 'resource_not_found', message: 'grader not found' });
  });
});
