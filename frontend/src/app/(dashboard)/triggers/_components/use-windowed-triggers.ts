'use client';

/**
 * @tier 3 — triggers-scoped. The Triggers page's rows with their stats over the selected window.
 * The server render carries the default window; another window is fetched here and replaces each
 * trigger's stats and the tenant's, while the rows themselves stay the server's.
 */

import { useEffect, useMemo, useState } from 'react';
import { listTriggers } from '../_services/triggers-api';
import type { TriggerRow } from '../_types';
import {
  DEFAULT_STATS_WINDOW,
  EMPTY_TRIGGER_STATS,
  toTenantStatsRow,
  toTriggerStatsRow,
} from '../_types/stats';
import type { StatsWindow, TenantStatsRow, TriggerStatsRow } from '../_types/stats';

interface Fetched {
  window: StatsWindow;
  byId: Map<string, TriggerStatsRow>;
  stats: TenantStatsRow | null;
}

export interface WindowedTriggers {
  triggers: TriggerRow[];
  stats: TenantStatsRow | null;
  /** `loadError`, or why the window's stats could not be read; the default window's stay on screen. */
  error: string | null;
}

export function useWindowedTriggers(
  triggers: TriggerRow[],
  stats: TenantStatsRow | null,
  range: StatsWindow,
  loadError: string | null,
): WindowedTriggers {
  const [fetched, setFetched] = useState<Fetched | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Refetched when the rows change too, so a refresh after an action refreshes the window's counts.
  useEffect(() => {
    if (range === DEFAULT_STATS_WINDOW) return;
    let live = true;
    void listTriggers({ window: range }).then((res) => {
      if (!live) return;
      setError(res.error);
      if (res.error !== null) return;
      setFetched({
        window: range,
        byId: new Map(res.triggers.map((t) => [t.id, toTriggerStatsRow(t.stats)])),
        stats: res.stats === null ? null : toTenantStatsRow(res.stats),
      });
    });
    return () => {
      live = false;
    };
  }, [range, triggers]);

  return useMemo(() => {
    if (range === DEFAULT_STATS_WINDOW || fetched?.window !== range) {
      return {
        triggers,
        stats,
        error: loadError ?? (range === DEFAULT_STATS_WINDOW ? null : error),
      };
    }
    return {
      triggers: triggers.map((t) => ({
        ...t,
        stats: fetched.byId.get(t.id) ?? EMPTY_TRIGGER_STATS,
      })),
      stats: fetched.stats,
      error: loadError,
    };
  }, [triggers, stats, range, fetched, error, loadError]);
}
