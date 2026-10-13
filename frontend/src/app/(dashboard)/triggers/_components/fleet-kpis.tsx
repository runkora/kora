'use client';

/** @tier 3 — triggers-scoped. Fleet health at a glance, above the trigger list. */

import { Grid, Icon, StatCard } from '@runkora/hades';
import type { DeliveryRow, TriggerRow } from '../_types';
import { fleetStats } from '../_types/fleet';

interface FleetKpisProps {
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
}

/** Latency in the unit that reads best at the value's magnitude. */
function formatMs(ms: number | null): string {
  if (ms === null) return '—';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

/**
 * FleetKpis summarises the whole trigger fleet.
 *
 * Every figure is computed from the same rows the list below renders, so a KPI
 * can never disagree with the detail it sits above. A number that contradicts
 * the rows beneath it is worse than no number, because it gets believed.
 */
export function FleetKpis({ triggers, deliveries }: FleetKpisProps) {
  const s = fleetStats(triggers, deliveries);
  const agents = new Set(triggers.map((t) => t.agentSlug)).size;

  return (
    <Grid cols={5} gap="comfortable">
      <StatCard
        label="ACTIVE TRIGGERS"
        value={String(s.activeTriggers)}
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
        // Null rather than 0% when nothing arrived: 0% reads as total failure
        // where in fact there was silence.
        value={s.successRate === null ? '—' : `${s.successRate}%`}
        detail={
          s.successRate === null
            ? 'no deliveries yet'
            : `${s.started24h.toLocaleString('en-GB')} ok · ${s.failed24h} failed`
        }
        icon={<Icon name="circle-check" size="2xs" />}
      />
      <StatCard
        label="NEEDS ATTENTION"
        value={String(s.needsAttention)}
        detail={s.needsAttention === 0 ? 'nothing failing' : 'triggers with failures'}
        tone={s.needsAttention > 0 ? 'error' : 'default'}
        detailTone={s.needsAttention > 0 ? 'error' : 'default'}
        icon={<Icon name="circle-x" size="2xs" />}
      />
      <StatCard
        label="AVG DISPATCH"
        value={formatMs(s.dispatchMs)}
        detail={s.dispatchP95Ms === null ? 'no dispatches yet' : `p95 ${formatMs(s.dispatchP95Ms)}`}
        icon={<Icon name="gauge" size="2xs" />}
      />
    </Grid>
  );
}
