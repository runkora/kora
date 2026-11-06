'use client';

/** @tier 3 — triggers-scoped. The tenant's trigger stats at a glance, above the trigger list. */

import { Grid, Icon, StatCard } from '@runkora/hades';
import type { TriggerRow } from '../_types';
import { needsAttention, successRate } from '../_types/stats';
import type { TenantStatsRow } from '../_types/stats';

interface TriggerStatsKpisProps {
  triggers: TriggerRow[];
  /** Null when the API answered without tenant stats. */
  stats: TenantStatsRow | null;
}

/** Latency in the unit that reads best at the value's magnitude. */
function formatMs(ms: number | null): string {
  if (ms === null) return '—';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

const NO_STATS: TenantStatsRow = {
  deliveries24h: 0,
  started24h: 0,
  failed24h: 0,
  dispatchP50Ms: null,
  dispatchP95Ms: null,
};

export function TriggerStatsKpis({ triggers, stats }: TriggerStatsKpisProps) {
  const s = stats ?? NO_STATS;
  const rate = successRate(s);
  const attention = triggers.filter(needsAttention).length;
  // A routed trigger reaches each of its candidates.
  const agents = new Set(
    triggers.flatMap((t) => (t.routing !== null ? t.routing.candidates : [t.agentTypeId])),
  ).size;

  return (
    <Grid cols={5} gap="comfortable">
      <StatCard
        label="ACTIVE TRIGGERS"
        value={String(triggers.filter((t) => t.enabled).length)}
        detail={`across ${agents} ${agents === 1 ? 'agent' : 'agents'}`}
        icon={<Icon name="webhook" size="2xs" />}
      />
      <StatCard
        label="DELIVERIES · 24H"
        value={s.deliveries24h.toLocaleString('en-GB')}
        detail="arrivals that authenticated"
        icon={<Icon name="activity" size="2xs" />}
      />
      <StatCard
        label="SUCCESS RATE"
        value={rate === null ? '—' : `${rate}%`}
        detail={
          rate === null
            ? 'no deliveries yet'
            : `${s.started24h.toLocaleString('en-GB')} ok · ${s.failed24h} failed`
        }
        icon={<Icon name="circle-check" size="2xs" />}
      />
      <StatCard
        label="NEEDS ATTENTION"
        value={String(attention)}
        detail={attention === 0 ? 'nothing failing' : 'triggers with failures'}
        tone={attention > 0 ? 'error' : 'default'}
        detailTone={attention > 0 ? 'error' : 'default'}
        icon={<Icon name="circle-x" size="2xs" />}
      />
      <StatCard
        label="AVG DISPATCH"
        value={formatMs(s.dispatchP50Ms)}
        detail={s.dispatchP95Ms === null ? 'no dispatches yet' : `p95 ${formatMs(s.dispatchP95Ms)}`}
        icon={<Icon name="gauge" size="2xs" />}
      />
    </Grid>
  );
}
