import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchRunPage, fetchRunStats } from './agents-api';
import { AGENT_ROWS, RAW_RUNS } from '../_components/agents-mock-data';

const respond = (body: unknown, status = 200) =>
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  );

afterEach(() => vi.restoreAllMocks());

describe('fetchRunPage', () => {
  it('maps the page to rows and keeps the next cursor', async () => {
    respond({ data: RAW_RUNS.slice(0, 2), next_cursor: 'next' });
    const page = await fetchRunPage('/api/agents/runs', AGENT_ROWS);
    expect(page.error).toBeNull();
    expect(page.nextCursor).toBe('next');
    expect(page.data.map((r) => r.id)).toEqual(RAW_RUNS.slice(0, 2).map((r) => r.id));
  });

  it('reports the server message on failure', async () => {
    respond({ message: 'limit must be a positive integer' }, 400);
    const page = await fetchRunPage('/api/agents/runs', AGENT_ROWS);
    expect(page).toEqual({ data: [], nextCursor: null, error: 'limit must be a positive integer' });
  });
});

describe('fetchRunStats', () => {
  it('unwraps the stats and asks for the period', async () => {
    const spy = respond({ data: { total: 4 } });
    const result = await fetchRunStats('30d');
    expect(result).toEqual({ data: { total: 4 }, error: null });
    expect(spy.mock.calls[0][0]).toBe('/api/agents/runs/stats?period=30d');
  });

  it('gives null stats on failure', async () => {
    respond({ message: 'down' }, 502);
    expect(await fetchRunStats('7d')).toEqual({ data: null, error: 'down' });
  });
});
