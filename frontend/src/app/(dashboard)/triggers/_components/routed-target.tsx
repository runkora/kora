'use client';

/** @tier 3 — triggers-scoped. The Target of a routed trigger in its drawer: the
 * candidate agents kora chooses between, and the settings it routes by. */

import { FieldRow, Icon, IconTile, Stack, Text } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import type { TriggerRoutingRow } from '../_types';
import type { RoutingAgentName } from './delivery-routing';

const CONFIDENCE_LABEL: Record<TriggerRoutingRow['confidence'], string> = {
  strict: 'Strict',
  balanced: 'Balanced',
  loose: 'Loose',
};

/** One line on how a routed trigger decides, for the drawer and the list. */
export function routingSummary(routing: TriggerRoutingRow): string {
  const joins = routing.joinRuns ? 'joins open runs' : 'never joins runs';
  return `${routing.model} · ${CONFIDENCE_LABEL[routing.confidence]} · ${joins}`;
}

export function RoutedTarget({
  routing,
  agents,
  limits,
}: {
  routing: TriggerRoutingRow;
  agents: RoutingAgentName[];
  /** The parallel and overflow line shown beside the first candidate. */
  limits: string;
}) {
  const { productName } = useBrand();
  const fallbackName = (id: string) => {
    const a = agents.find((x) => x.id === id);
    return a?.name?.trim() || a?.slug || id;
  };
  return (
    <Stack gap="snug">
      <FieldRow
        leading={
          <IconTile tone="accent">
            <Icon name="route" size="xs" label="" />
          </IconTile>
        }
        actions={
          <Text size="micro" color="subtle">
            {limits}
          </Text>
        }
      >
        <Stack gap="none">
          <Text size="supporting" weight="emphasis">
            {productName} chooses between {routing.candidates.length} agents
          </Text>
          <Text mono size="micro" color="subtle">
            {routingSummary(routing)}
          </Text>
        </Stack>
      </FieldRow>
      {routing.candidates.map((id) => {
        const a = agents.find((x) => x.id === id);
        const label = a?.name?.trim() || a?.slug || id;
        return (
          <FieldRow
            key={id}
            leading={
              <IconTile tone="muted">
                <Text mono size="caption" weight="heavy">
                  {label.charAt(0).toUpperCase()}
                </Text>
              </IconTile>
            }
            actions={
              routing.fallbackAgentTypeId === id ? (
                <Text size="micro" color="subtle">
                  fallback
                </Text>
              ) : undefined
            }
          >
            <Stack gap="none">
              <Text size="supporting" weight="emphasis">
                {label}
              </Text>
              {a !== undefined && (
                <Text mono size="micro" color="subtle">
                  agt/{a.slug}
                </Text>
              )}
            </Stack>
          </FieldRow>
        );
      })}
      <Text size="caption" color="subtle">
        {routing.fallbackAgentTypeId === null
          ? `When ${productName} is unsure, no run starts.`
          : `When ${productName} is unsure, ${fallbackName(routing.fallbackAgentTypeId)} takes the delivery.`}
      </Text>
      {routing.joinRuns && routing.joinKey !== '' && (
        <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
          <Text size="caption" color="subtle">
            Join key
          </Text>
          <Text mono size="caption" truncate>
            {routing.joinKey}
          </Text>
        </Stack>
      )}
    </Stack>
  );
}
