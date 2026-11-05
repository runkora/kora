'use client';

/** @tier 3 — triggers-scoped. How kora routed one delivery of a "Let kora
 * choose" trigger: the agent and run it reached, why, and every score. */

import type * as React from 'react';
import {
  Badge,
  Callout,
  Card,
  CardContent,
  Icon,
  IconTile,
  Progress,
  SectionLabel,
  Stack,
  Text,
} from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import { confidencePresetOf } from '../_types';
import type { DeliveryRoutingRow, TriggerRoutingConfidence } from '../_types';

/** An agent the panel can name; an id it cannot find is shown as the id. */
export interface RoutingAgentName {
  id: string;
  slug: string;
  name?: string;
}

const PRESET_LABEL: Record<TriggerRoutingConfidence, string> = {
  strict: 'Strict',
  balanced: 'Balanced',
  loose: 'Loose',
};

const score = (p: number) => p.toFixed(2);

/** The threshold in words, e.g. "Balanced (0.70)". */
export function thresholdLabel(threshold: number): string {
  const preset = confidencePresetOf(threshold);
  return preset === null ? score(threshold) : `${PRESET_LABEL[preset]} (${score(threshold)})`;
}

/** What the fallback row says: whether the fallback took the delivery, and why. */
export function fallbackText(d: DeliveryRoutingRow): string {
  if (d.fallbackReason === 'run_closed')
    return 'Not used. The chosen run closed first, so a new run of its agent started.';
  if (d.error !== null) {
    return d.outcome === 'no_run'
      ? 'Not set. The routing decision failed.'
      : 'Used. The routing decision failed.';
  }
  const best = d.probability ?? d.agentScores[0]?.probability;
  const below =
    best === undefined || d.threshold === null
      ? 'No candidate scored'
      : `Best score ${score(best)} is below ${thresholdLabel(d.threshold)}`;
  if (d.outcome === 'fallback_run') return `Used. ${below}.`;
  if (d.outcome === 'no_run') return `Not set. ${below}.`;
  return 'Not used';
}

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

export interface DeliveryRoutingProps {
  routing: DeliveryRoutingRow;
  /** The run the delivery started or joined, from its delivery run row. */
  runId: string | null;
  agents: RoutingAgentName[];
}

export function DeliveryRouting({ routing: d, runId, agents }: DeliveryRoutingProps) {
  const { productName } = useBrand();
  const named = (id: string) => {
    const a = agents.find((x) => x.id === id);
    return { name: a?.name?.trim() || a?.slug || id, slug: a?.slug ?? id };
  };
  const agent = d.outcome === 'no_run' || d.agentTypeId === null ? null : named(d.agentTypeId);
  const fallback = d.outcome === 'fallback_run';
  const unrouted = d.outcome === 'no_run';
  const confidence =
    !fallback && agent !== null && !d.keyMatched && d.model !== null ? d.probability : null;
  return (
    <Stack gap="cozy">
      <SectionLabel>Routing</SectionLabel>
      <Card transparent>
        <CardContent padded compact>
          <Stack direction="horizontal" align="center" gap="cozy">
            <IconTile tone={agent === null ? 'muted' : 'accent'}>
              <Icon name={agent === null ? 'x-circle' : 'route'} size="xs" label="" />
            </IconTile>
            <Stack gap="tight" grow minWidth="none">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text as="span" size="supporting" weight="heavy">
                  {agent === null ? 'No run started' : agent.name}
                </Text>
                {fallback && (
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
                {agent !== null
                  ? agent.slug
                  : d.error !== null
                    ? 'the decision failed and no fallback is set'
                    : `${productName} was not sure enough to route`}
              </Text>
            </Stack>
            {confidence !== null && (
              <Stack align="end" gap="tight" shrink={false}>
                <Text as="span" size="supporting" mono weight="heavy" tabular>
                  {score(confidence)}
                </Text>
                <Text as="span" size="micro" color="subtle">
                  confidence
                </Text>
              </Stack>
            )}
          </Stack>
        </CardContent>
      </Card>

      {d.error !== null && (
        <Callout variant="warning">
          <Stack gap="tight" minWidth="none">
            <Text size="caption" weight="emphasis">
              {unrouted
                ? 'The routing decision failed, and the trigger has no fallback agent, so no run started.'
                : 'The routing decision failed, so the delivery went to the fallback agent.'}
            </Text>
            <Text mono size="micro" color="subtle" preserveWhitespace>
              {d.error}
            </Text>
          </Stack>
        </Callout>
      )}

      <Stack gap="snug">
        <Row label="Decision">
          {runId === null ? (
            <Text size="caption">No run started</Text>
          ) : (
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption">{d.outcome === 'joined_run' ? 'Joined run' : 'New run'}</Text>
              <Text mono size="caption" color="accent">
                {runId}
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
          <Text size="caption" color={fallback || unrouted ? 'status-degraded' : undefined}>
            {fallbackText(d)}
          </Text>
        </Row>
        <Row label="Model">
          {d.model === null ? (
            <Text size="caption" color="subtle">
              Not called. The key decided the run.
            </Text>
          ) : (
            <Text mono size="caption">
              {d.model}
            </Text>
          )}
        </Row>
      </Stack>

      {d.agentScores.length > 0 && (
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" justify="between">
            <Text size="caption" color="muted">
              Scores
            </Text>
            {d.threshold !== null && (
              <Text size="micro" color="subtle">
                Routes at {thresholdLabel(d.threshold)} or above
              </Text>
            )}
          </Stack>
          {d.agentScores.map((s, i) => {
            const label = named(s.agentTypeId).name;
            return (
              <Stack key={s.agentTypeId} direction="horizontal" align="center" gap="cozy">
                <Stack shrink={false} minWidth="form-field-sm" maxWidth="form-field-sm">
                  <Text
                    size="caption"
                    weight={i === 0 ? 'heavy' : undefined}
                    color={i === 0 ? undefined : 'muted'}
                    truncate
                  >
                    {label}
                  </Text>
                </Stack>
                <Stack grow minWidth="none">
                  <Progress
                    size="sm"
                    value={s.probability * 100}
                    label={`${label} score`}
                    variant={
                      i === 0 && d.threshold !== null && s.probability >= d.threshold
                        ? 'default'
                        : 'pending'
                    }
                  />
                </Stack>
                <Text mono size="caption" tabular color={i === 0 ? undefined : 'muted'}>
                  {score(s.probability)}
                </Text>
              </Stack>
            );
          })}
        </Stack>
      )}
    </Stack>
  );
}
