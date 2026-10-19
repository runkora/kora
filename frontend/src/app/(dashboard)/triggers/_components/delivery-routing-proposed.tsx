'use client';

/** @tier 3 — triggers-scoped. Proposed: how kora routed one delivery on a
 * "Let kora choose" trigger, shown above the delivery detail. Storybook only. */

import type * as React from 'react';
import {
  Badge,
  Button,
  Callout,
  Card,
  CardContent,
  Icon,
  IconTile,
  Progress,
  SectionLabel,
  SidePanel,
  SidePanelBody,
  SidePanelHeader,
  Stack,
  Text,
} from '@runkora/hades';
import { StatusBadge } from '@runkora/components';
import { DeliveryDetail } from './delivery-detail';
import { deliveryStatusTone, formatTimestampUtc } from '../_types';
import type { DeliveryRow } from '../_types';

export type ConfidencePresetName = 'Strict' | 'Balanced' | 'Loose';

export interface RoutingScore {
  agentSlug: string;
  agentName: string;
  probability: number;
}

/** What the platform records when it routes a delivery. */
export interface RoutingDecision {
  /** Highest first. Empty when the model was not called or did not answer. */
  scores: RoutingScore[];
  /** The agent that received the delivery; null when no run started. */
  agent: { slug: string; name: string } | null;
  run: { kind: 'new' | 'joined'; runId: string } | null;
  joinKey: string | null;
  keyMatched: boolean;
  fallbackFired: boolean;
  threshold: { preset: ConfidencePresetName; value: number };
  /** Null when the decision needed no model call. */
  model: { id: string; name: string } | null;
  modelError: string | null;
}

const score = (p: number) => p.toFixed(2);

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack direction="horizontal" align="start" gap="cozy">
      <Stack shrink={false} minWidth="form-field-sm" maxWidth="form-field-sm">
        <Text size="caption" color="muted">
          {label}
        </Text>
      </Stack>
      <Stack grow minWidth="none">
        {children}
      </Stack>
    </Stack>
  );
}

function fallbackText(d: RoutingDecision): string {
  if (d.modelError !== null) return 'Used. The decision model did not answer.';
  const best = d.scores[0];
  const below =
    best === undefined
      ? 'No candidate scored'
      : `Best score ${score(best.probability)} is below ${d.threshold.preset} (${score(d.threshold.value)})`;
  if (d.fallbackFired) return `Used. ${below}.`;
  if (d.agent === null) return `Not set. ${below}.`;
  return 'Not used';
}

export function DeliveryRouting({ decision: d }: { decision: RoutingDecision }) {
  const top = d.scores[0];
  const confident = !d.fallbackFired && d.agent !== null && top !== undefined;
  return (
    <Stack gap="cozy">
      <SectionLabel>Routing</SectionLabel>
      <Card inset>
        <CardContent padded compact>
          <Stack direction="horizontal" align="center" gap="cozy">
            <IconTile tone={d.agent === null ? 'muted' : 'accent'}>
              <Icon name={d.agent === null ? 'x-circle' : 'route'} size="xs" label="" />
            </IconTile>
            <Stack gap="tight" grow minWidth="none">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text as="span" size="supporting" weight="heavy">
                  {d.agent === null ? 'No run started' : d.agent.name}
                </Text>
                {d.fallbackFired && d.agent !== null && (
                  <Badge variant="degraded" size="sm">
                    fallback
                  </Badge>
                )}
                {d.keyMatched && (
                  <Badge variant="secondary" size="sm">
                    joined by key
                  </Badge>
                )}
              </Stack>
              <Text as="span" size="micro" mono color="subtle">
                {d.agent === null ? 'kora was not sure enough to route' : d.agent.slug}
              </Text>
            </Stack>
            {confident && (
              <Stack align="end" gap="tight" shrink={false}>
                <Text as="span" size="supporting" mono weight="heavy" tabular>
                  {score(top.probability)}
                </Text>
                <Text as="span" size="micro" color="subtle">
                  confidence
                </Text>
              </Stack>
            )}
          </Stack>
        </CardContent>
      </Card>

      {d.modelError !== null && (
        <Callout variant="warning">
          <Stack gap="tight" minWidth="none">
            <Text size="caption" weight="emphasis">
              The decision model call failed, so the delivery went to the fallback agent.
            </Text>
            <Text mono size="micro" color="subtle" preserveWhitespace>
              {d.modelError}
            </Text>
          </Stack>
        </Callout>
      )}

      <Stack gap="snug">
        <Row label="Decision">
          {d.run === null ? (
            <Text size="caption">No run started</Text>
          ) : (
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption">{d.run.kind === 'new' ? 'New run' : 'Joined run'}</Text>
              <Text mono size="caption" color="accent">
                {d.run.runId}
              </Text>
            </Stack>
          )}
        </Row>
        <Row label="Key match">
          <Text size="caption" color={d.joinKey === null ? 'subtle' : undefined}>
            {d.joinKey === null
              ? 'Not set on this trigger'
              : d.keyMatched
                ? 'Matched an open run'
                : 'No open run for this key'}
          </Text>
        </Row>
        <Row label="Fallback">
          <Text
            size="caption"
            color={d.fallbackFired || d.agent === null ? 'status-degraded' : undefined}
          >
            {fallbackText(d)}
          </Text>
        </Row>
        <Row label="Model">
          {d.model === null ? (
            <Text size="caption" color="subtle">
              Not called. The key decided the run.
            </Text>
          ) : (
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption">{d.model.name}</Text>
              <Text mono size="micro" color="subtle">
                {d.model.id}
              </Text>
            </Stack>
          )}
        </Row>
      </Stack>

      {d.scores.length > 0 && (
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" justify="between">
            <Text size="caption" color="muted">
              Scores
            </Text>
            <Text size="micro" color="subtle">
              {d.threshold.preset} routes at {score(d.threshold.value)} or above
            </Text>
          </Stack>
          {d.scores.map((s, i) => (
            <Stack key={s.agentSlug} direction="horizontal" align="center" gap="cozy">
              <Stack shrink={false} minWidth="form-field-sm" maxWidth="form-field-sm">
                <Text
                  size="caption"
                  weight={i === 0 ? 'heavy' : undefined}
                  color={i === 0 ? undefined : 'muted'}
                  truncate
                >
                  {s.agentName}
                </Text>
              </Stack>
              <Stack grow minWidth="none">
                <Progress
                  size="sm"
                  value={s.probability * 100}
                  label={`${s.agentName} score`}
                  variant={i === 0 && s.probability >= d.threshold.value ? 'default' : 'pending'}
                />
              </Stack>
              <Text mono size="caption" tabular color={i === 0 ? undefined : 'muted'}>
                {score(s.probability)}
              </Text>
            </Stack>
          ))}
        </Stack>
      )}
    </Stack>
  );
}

export interface DeliveryDetailPanelProposedProps {
  delivery: DeliveryRow;
  decision: RoutingDecision;
  onClose: () => void;
}

/** The real panel's header and body, with the routing decision above the detail. */
export function DeliveryDetailPanelProposed({
  delivery,
  decision,
  onClose,
}: DeliveryDetailPanelProposedProps) {
  return (
    <SidePanel open onClose={onClose} size="md">
      <SidePanelHeader
        density="compact"
        title={delivery.eventType || delivery.kind}
        description={`Delivery ${delivery.id}`}
      />
      <SidePanelBody padding="compact">
        <Stack gap="comfortable">
          <Stack direction="horizontal" align="center" gap="cozy">
            <StatusBadge status={deliveryStatusTone(delivery.status)} label={delivery.status} />
            <Text size="caption" color="muted">
              {formatTimestampUtc(delivery.receivedAt)}
            </Text>
          </Stack>
          <DeliveryRouting decision={decision} />
          <DeliveryDetail
            delivery={delivery}
            onRedeliver={async () => ({ deliveryId: null, error: null })}
          />
          <Stack direction="horizontal" justify="end">
            <Button variant="ghost" size="xs">
              Open trigger
              <Icon name="arrow-right" size="2xs" />
            </Button>
          </Stack>
        </Stack>
      </SidePanelBody>
    </SidePanel>
  );
}
