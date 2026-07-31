'use client';

/**
 * @tier 3 — triggers-scoped. Asks the scheduler what a cron expression will
 * actually do next.
 *
 * The builder's own sentence is a description of what was configured; this is
 * the scheduler's answer about when it will fire. Only the second can be
 * trusted for exact times, so the two are kept separate rather than blended:
 * when the lookup fails, the console shows no times at all rather than a guess
 * that could differ from the thing that will really run.
 */

import { useEffect, useState } from 'react';
import type { SchedulePreviewFire } from '@orbit/shared/api';
import { fetchSchedulePreview } from '../../_services/triggers-api';

/** Long enough that typing a cron expression does not fire a call per keystroke. */
const DEBOUNCE_MS = 400;

interface PreviewState {
  /** The expression and zone these fires belong to. */
  key: string;
  fires: SchedulePreviewFire[];
}

export interface SchedulePreview {
  /** Empty whenever the answer is unknown, stale, or the lookup failed. */
  fires: SchedulePreviewFire[];
  /** A lookup is in flight for the current expression. */
  loading: boolean;
}

export function useSchedulePreview(
  cronExpression: string,
  timezone: string,
  enabled = true,
): SchedulePreview {
  const [state, setState] = useState<PreviewState>({ key: '', fires: [] });
  const key = `${cronExpression.trim()}|${timezone}`;

  useEffect(() => {
    if (!enabled || !cronExpression.trim()) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void fetchSchedulePreview(cronExpression.trim(), timezone, controller.signal).then(
        (result) => {
          if (controller.signal.aborted) return;
          setState({ key, fires: result?.next_fires ?? [] });
        },
      );
    }, DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [cronExpression, timezone, enabled, key]);

  // Fires computed for a different expression are not this expression's fires.
  // Deriving rather than clearing in an effect means a changed cron never shows
  // the previous one's times, not even for a frame.
  const fresh = state.key === key;
  return { fires: fresh ? state.fires : [], loading: enabled && !fresh };
}
