/**
 * Integration tests for POST /api/triggers.
 *
 * The route copies an explicit list of fields to orbit-triggers. Each field the
 * backend accepts needs a test that the route forwards it.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraTriggersUrl: 'http://test-triggers-svc' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { POST } from './route';
import { getSession } from '@runkora/server/session';

const TRIGGERS_URL = 'http://test-triggers-svc/triggers';

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['triggers:write'], evaluatedAt: 0 },
  };
}

function makePostRequest(body: unknown): NextRequest {
  return new NextRequest('http://localhost/api/triggers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

describe('POST /api/triggers', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
  });

  it('forwards sender_restriction to orbit-triggers', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(TRIGGERS_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 't-1', ...received }, { status: 201 });
      }),
    );

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'comment-review',
        agent_type_id: 'agent-1',
        event_type: 'issue_comment',
        prompt_template: '{{ comment.body }}',
        sender_restriction: { logins: ['octocat'], associations: ['MEMBER'] },
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(201);
    expect(received?.sender_restriction).toEqual({
      logins: ['octocat'],
      associations: ['MEMBER'],
    });
  });

  it('forwards coalesce to orbit-triggers unchanged', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(TRIGGERS_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 't-1', ...received }, { status: 201 });
      }),
    );

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'pr-review',
        agent_type_id: 'agent-1',
        event_type: 'pull_request',
        prompt_template: '{{ pull_request.title }}',
        coalesce: { key: '{{ pull_request.number }}', window_seconds: 30 },
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(201);
    expect(received?.coalesce).toEqual({ key: '{{ pull_request.number }}', window_seconds: 30 });
  });

  it('forwards match_contains to orbit-triggers', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(TRIGGERS_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 't-1', ...received }, { status: 201 });
      }),
    );

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'kora-mentions',
        agent_type_id: 'agent-1',
        event_type: 'issue_comment',
        prompt_template: '{{ comment.body }}',
        match_contains: { 'comment.body': ['@kora-agent'] },
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(201);
    expect(received?.match_contains).toEqual({ 'comment.body': ['@kora-agent'] });
  });

  it('forwards a routed trigger without an agent', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(TRIGGERS_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 't-1', ...received }, { status: 201 });
      }),
    );
    const routing = {
      candidates: ['agent-1', 'agent-2'],
      model: 'liquid/d1',
      confidence: 'balanced',
      fallback_agent_type_id: null,
      join_runs: true,
      join_key: '{{ issue.number }}',
    };

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'route-github-work',
        target_kind: 'routed',
        routing,
        event_type: 'issues',
        prompt_template: '{{ issue.title }}',
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(201);
    expect(received?.target_kind).toBe('routed');
    expect(received?.routing).toEqual(routing);
    expect(received?.agent_type_id).toBeUndefined();
  });

  it('passes the missing scopes of a refused routed trigger through', async () => {
    server.use(
      http.post(TRIGGERS_URL, () =>
        HttpResponse.json(
          {
            code: 'forbidden',
            message: 'caller does not hold the scopes this routed trigger would be granted',
            details: { missing_scopes: ['agent:coder:run'], grant_id: 'grant-internal' },
          },
          { status: 403 },
        ),
      ),
    );

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'route-github-work',
        target_kind: 'routed',
        routing: { candidates: ['a', 'b'], model: 'liquid/d1', confidence: 'balanced' },
        event_type: 'issues',
        prompt_template: '{{ issue.title }}',
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(403);
    expect((await res.json()).details).toEqual({ missing_scopes: ['agent:coder:run'] });
  });

  it('rejects a webhook trigger without an event type', async () => {
    let called = false;
    server.use(
      http.post(TRIGGERS_URL, () => {
        called = true;
        return HttpResponse.json({}, { status: 201 });
      }),
    );

    const res = await POST(
      makePostRequest({
        kind: 'webhook',
        source_id: 'src-1',
        name: 'comment-review',
        agent_type_id: 'agent-1',
        prompt_template: '{{ comment.body }}',
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(400);
    expect(called).toBe(false);
  });
});
