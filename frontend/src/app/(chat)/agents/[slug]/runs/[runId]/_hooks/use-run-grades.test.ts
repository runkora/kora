import { renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { server } from '@runkora/test-utils/msw/server';
import { RUN_GRADES } from '../_lib/run-grades-mock-data';
import { useRunGrades } from './use-run-grades';

/** Serves the run's grades: none for the first `empty` reads, then `RUN_GRADES.pass`. */
function serveGrades(empty: number) {
  let reads = 0;
  server.use(
    http.get('/api/agents/runs/run-1/grades', () => {
      reads += 1;
      return HttpResponse.json({ grades: reads > empty ? RUN_GRADES.pass : [] });
    }),
    http.get('/api/agents/types/at-1/graders', () =>
      HttpResponse.json({
        graders: [
          {
            name: 'review-outcome',
            description: '',
            min_pass_rate: null,
            version: 'v',
            status: 'no_minimum',
            passed: 0,
            graded: 0,
            too_large: 0,
          },
        ],
      }),
    ),
  );
  return () => reads;
}

describe('useRunGrades', () => {
  afterEach(() => vi.useRealTimers());

  it("names the agent's Graders while the run is live", async () => {
    serveGrades(0);
    const { result } = renderHook(() => useRunGrades('run-1', 'at-1', false));
    await waitFor(() =>
      expect(result.current).toEqual({ ended: false, graders: ['review-outcome'] }),
    );
  });

  it('reads an ended run once', async () => {
    const reads = serveGrades(0);
    const { result } = renderHook(() => useRunGrades('run-1', 'at-1', true));
    await waitFor(() => expect(result.current).toEqual({ ended: true, grades: RUN_GRADES.pass }));
    expect(reads()).toBe(1);
  });

  it('reads again until the first grade lands when the run ends on the page', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const reads = serveGrades(2);
    const { result, rerender } = renderHook(({ ended }) => useRunGrades('run-1', 'at-1', ended), {
      initialProps: { ended: false },
    });
    rerender({ ended: true });
    await waitFor(() => expect(reads()).toBe(1));
    expect(result.current).toEqual({ ended: true, grades: [] });
    await vi.advanceTimersByTimeAsync(10_000);
    await waitFor(() => expect(result.current).toEqual({ ended: true, grades: RUN_GRADES.pass }));
    expect(reads()).toBe(3);
  });
});
