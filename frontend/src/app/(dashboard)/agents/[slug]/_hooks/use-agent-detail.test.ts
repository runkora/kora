import { describe, expect, it } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import type { AgentTypeRow, RunRow } from '../../_types';
import { useAgentDetail } from './use-agent-detail';

const agent = { id: 'at-1', slug: 'coder', name: 'Coder', state: 'active' } as AgentTypeRow;
const raw = (id: string, state: string) => ({
  id,
  tenant_id: 't',
  agent_type_id: 'at-1',
  state,
  created_by: 'u',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
});
const row = (id: string) => ({ id, state: 'completed' }) as RunRow;

/** A promise and the function that settles it. */
function gate() {
  let open!: () => void;
  const opened = new Promise<void>((resolve) => (open = resolve));
  return { open, opened };
}

describe('useAgentDetail paging across a filter switch', () => {
  it('does not page the new filter with the old filter cursor', async () => {
    const firstPage = gate();
    const queries: string[] = [];
    server.use(
      http.get('/api/agents/types/:id/runs', async ({ request }) => {
        const q = new URL(request.url).searchParams;
        queries.push(q.toString());
        if (!q.get('cursor')) {
          await firstPage.opened;
          return HttpResponse.json({ data: [raw('f1', 'failed')], next_cursor: 'FAILED-C1' });
        }
        return HttpResponse.json({ data: [raw('f2', 'failed')], next_cursor: null });
      }),
    );
    const { result } = renderHook(() =>
      useAgentDetail(agent, [row('a1')], 'ALL-C1', null, 'success'),
    );

    act(() => result.current.setRunFilter('failed'));
    // The old cursor is gone while the new first page loads, so this is a no-op.
    await act(() => result.current.loadMoreRuns());
    firstPage.open();
    await waitFor(() => expect(result.current.runs.nextCursor).toBe('FAILED-C1'));
    await act(() => result.current.loadMoreRuns());

    expect(queries.some((q) => q.includes('ALL-C1'))).toBe(false);
    expect(result.current.runs.rows.map((r) => r.id)).toEqual(['f1', 'f2']);
    expect(result.current.runs.nextCursor).toBeNull();
  });

  it('pages the new filter while a load more for the old one is still in flight', async () => {
    const oldMore = gate();
    const queries: string[] = [];
    server.use(
      http.get('/api/agents/types/:id/runs', async ({ request }) => {
        const q = new URL(request.url).searchParams;
        queries.push(q.toString());
        if (q.get('cursor') === 'ALL-C1') {
          await oldMore.opened;
          return HttpResponse.json({ data: [raw('a2', 'completed')], next_cursor: null });
        }
        if (q.get('cursor') === 'FAILED-C1') {
          return HttpResponse.json({ data: [raw('f2', 'failed')], next_cursor: null });
        }
        return HttpResponse.json({ data: [raw('f1', 'failed')], next_cursor: 'FAILED-C1' });
      }),
    );
    const { result } = renderHook(() =>
      useAgentDetail(agent, [row('a1')], 'ALL-C1', null, 'success'),
    );

    let stale!: Promise<void>;
    act(() => {
      stale = result.current.loadMoreRuns();
    });
    act(() => result.current.setRunFilter('failed'));
    await waitFor(() => expect(result.current.runs.nextCursor).toBe('FAILED-C1'));
    await act(() => result.current.loadMoreRuns());
    oldMore.open();
    await act(() => stale);

    expect(queries.filter((q) => q.includes('FAILED-C1'))).toHaveLength(1);
    expect(result.current.runs.rows.map((r) => r.id)).toEqual(['f1', 'f2']);
  });
});
