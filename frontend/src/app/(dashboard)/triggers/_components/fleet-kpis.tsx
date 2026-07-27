/** @tier 3 — triggers-scoped. Fleet health at a glance, above the trigger list. */

'use client';

import { Card, CardContent, Grid, Icon, Stack, Text } from '@orbit/terra';
import type { DeliveryRow, TriggerRow } from '../_types';
import { fleetStats } from '../_types/fleet';

interface FleetKpisProps {
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
}

/** One KPI tile. Kept local: these are compact figures, not the Tier-2 MetricCard. */
function Kpi({
  icon,
  label,
  value,
  detail,
  tone,
}: {
  icon: 'zap' | 'activity' | 'circle-check' | 'triangle-alert';
  label: string;
  value: string;
  detail: string;
  tone?: 'error';
}) {
  return (
    <Card>
      <CardContent>
        <Stack gap="tight">
          <Stack direction="horizontal" gap="snug" align="center">
            <Icon name={icon} size="xs" color={tone === 'error' ? 'status-error' : undefined} />
            <Text size="supporting" color="muted">
              {label}
            </Text>
          </Stack>
          <Text size="lead">{value}</Text>
          <Text size="caption" color="muted">
            {detail}
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}

/**
 * FleetKpis summarises the whole trigger fleet.
 *
 * Every figure is computed from the same rows the table below renders, so a KPI
 * can never disagree with the list it sits above. A number that contradicts the
 * detail beneath it is worse than no number, because it gets believed.
 */
export function FleetKpis({ triggers, deliveries }: FleetKpisProps) {
  const stats = fleetStats(triggers, deliveries);

  return (
    <Grid cols={4} gap="comfortable">
      <Kpi
        icon="zap"
        label="Active triggers"
        value={String(stats.activeTriggers)}
        detail={`of ${stats.totalTriggers} configured`}
      />
      <Kpi
        icon="activity"
        label="Deliveries (24h)"
        value={String(stats.deliveries24h)}
        detail="arrivals that authenticated"
      />
      <Kpi
        icon="circle-check"
        label="Started a run"
        // Null rather than 0% when nothing arrived: 0% would read as total
        // failure where in fact there was silence.
        value={stats.successRate === null ? '—' : `${stats.successRate}%`}
        detail={stats.successRate === null ? 'no deliveries yet' : 'of decided outcomes'}
      />
      <Kpi
        icon="triangle-alert"
        label="Needs attention"
        value={String(stats.needsAttention)}
        detail={stats.needsAttention === 0 ? 'nothing failing' : 'paused or failing'}
        tone={stats.needsAttention > 0 ? 'error' : undefined}
      />
    </Grid>
  );
}
