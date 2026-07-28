'use client';

import { useState } from 'react';

/** @tier 3 — triggers-scoped. Shared Target step: the agent this trigger runs,
 * the concurrency ceiling, and what happens to the overlap beyond it. */

import {
  Button,
  Callout,
  Card,
  CardContent,
  FormField,
  Grid,
  Icon,
  IconTile,
  Input,
  RadioCard,
  RadioGroup,
  RadioGroupItem,
  SectionLabel,
  Stack,
  Text,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { LIMITS, parallelWord } from './wizard-state';
import type { StepComponentProps, TriggerKind, TriggerOverflow, TriggerWizardAgent } from './types';

interface OverflowOption {
  id: TriggerOverflow;
  label: string;
  icon: IconName;
  description: string;
  /** The kinds this behaviour suits, called out in the card. */
  best: TriggerKind[];
}

const OVERFLOW_OPTIONS: OverflowOption[] = [
  {
    id: 'wait',
    label: 'Wait',
    icon: 'clock',
    description: 'Hold deliveries in order and run them as capacity frees.',
    best: ['webhook', 'manual'],
  },
  {
    id: 'replace',
    label: 'Replace',
    icon: 'layers',
    description: 'Keep only the newest waiting delivery; older ones are superseded.',
    best: ['scheduled'],
  },
  {
    id: 'refuse',
    label: 'Refuse',
    icon: 'x-circle',
    description: 'Reject the delivery with 429 so the sender decides what to do.',
    best: [],
  },
];

function initial(agent: TriggerWizardAgent): string {
  const source = agent.name?.trim() || agent.slug;
  return (source.charAt(0) || '?').toUpperCase();
}

/** Above this many agents the list gains a search box and scrolls. */
const AGENT_SEARCH_THRESHOLD = 6;

export function StepTarget({ state, patch, context, definition }: StepComponentProps) {
  const { agents } = context;
  const [agentQuery, setAgentQuery] = useState('');
  const query = agentQuery.trim().toLowerCase();
  const visibleAgents =
    query === ''
      ? agents
      : agents.filter(
          (a) =>
            a.slug.toLowerCase().includes(query) || (a.name ?? '').toLowerCase().includes(query),
        );
  const overflowDetail =
    state.overflow === 'wait'
      ? `Further deliveries wait in order, up to ${state.maxQueueDepth} of them.`
      : state.overflow === 'replace'
        ? 'While one runs, a newer delivery replaces the one waiting: only the latest is kept.'
        : 'Beyond that, deliveries are refused with 429 and the caller decides what to do.';

  return (
    <Stack gap="relaxed">
      <Stack gap="cozy">
        <SectionLabel>Agent to run</SectionLabel>
        {agents.length === 0 && (
          <Callout variant="warning">
            No agents are available to bind. Create an autonomous agent first.
          </Callout>
        )}
        {agents.length > AGENT_SEARCH_THRESHOLD && (
          <Stack direction="horizontal" align="center" gap="cozy">
            <Stack grow maxWidth="search-field">
              <Input
                compact
                value={agentQuery}
                onChange={(e) => setAgentQuery(e.target.value)}
                placeholder="Search agents…"
                aria-label="Search agents"
              />
            </Stack>
            <Text as="span" size="caption" color="subtle">
              {visibleAgents.length} of {agents.length}
            </Text>
          </Stack>
        )}
        <RadioGroup
          value={state.agentTypeId}
          onValueChange={(v) => patch({ agentTypeId: v })}
          aria-label="Agent"
        >
          {/* Bounded height: past ~5 rows the list scrolls rather than pushing
              the concurrency controls off screen. The scrolling wrapper holds
              ONE child stack: putting the cards directly inside a bounded flex
              column would let flexbox compress them to fit instead of
              scrolling, clipping every row. */}
          <Stack maxHeight="scroll-lg" overflowY="auto">
            <Stack gap="snug">
              {visibleAgents.length === 0 && (
                <Text size="caption" color="subtle">
                  No agents match this search.
                </Text>
              )}
              {visibleAgents.map((a) => {
                const delegated = a.identityMode === 'delegated';
                return (
                  <Card
                    key={a.id}
                    interactive={!delegated}
                    subtle={delegated}
                    selected={state.agentTypeId === a.id}
                    onClick={delegated ? undefined : () => patch({ agentTypeId: a.id })}
                  >
                    <CardContent padded compact>
                      <Stack direction="horizontal" align="center" gap="cozy">
                        <IconTile tone={state.agentTypeId === a.id ? 'accent' : 'muted'}>
                          <Text as="span" size="caption" mono weight="heavy">
                            {initial(a)}
                          </Text>
                        </IconTile>
                        <Stack gap="tight" grow minWidth="none">
                          <Text as="span" size="supporting" weight="heavy">
                            {a.name?.trim() || a.slug}
                          </Text>
                          <Text as="span" size="micro" mono color="subtle">
                            {a.slug}
                          </Text>
                        </Stack>
                        {delegated ? (
                          <Text as="span" size="micro" color="subtle">
                            delegated · started by people
                          </Text>
                        ) : (
                          <RadioGroupItem value={a.id} aria-label={a.name?.trim() || a.slug} />
                        )}
                      </Stack>
                    </CardContent>
                  </Card>
                );
              })}
            </Stack>
          </Stack>
        </RadioGroup>
      </Stack>

      <Stack gap="snug">
        <SectionLabel>Overlapping runs</SectionLabel>
        <Text size="caption" color="subtle">
          A trigger can fire faster than the agent finishes. This decides what happens to the
          overlap; the limits apply per trigger.
        </Text>
        <Card inset>
          <CardContent padded compact>
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Stack gap="tight" grow minWidth="none">
                <Text as="span" size="supporting" weight="heavy">
                  Max parallel runs
                </Text>
                <Text as="span" size="caption" color="muted">
                  {parallelWord(state.maxParallel)}
                </Text>
              </Stack>
              <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
                <Button
                  size="icon-sm"
                  variant="secondary"
                  aria-label="Fewer parallel runs"
                  disabled={state.maxParallel <= LIMITS.parallel.min}
                  onClick={() =>
                    patch({ maxParallel: Math.max(LIMITS.parallel.min, state.maxParallel - 1) })
                  }
                >
                  <Icon name="minus" size="xs" />
                </Button>
                <Text as="span" size="supporting" mono weight="heavy" tabular>
                  {state.maxParallel}
                </Text>
                <Button
                  size="icon-sm"
                  variant="secondary"
                  aria-label="More parallel runs"
                  disabled={state.maxParallel >= LIMITS.parallel.max}
                  onClick={() =>
                    patch({ maxParallel: Math.min(LIMITS.parallel.max, state.maxParallel + 1) })
                  }
                >
                  <Icon name="plus" size="xs" />
                </Button>
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </Stack>

      <Stack gap="snug">
        <SectionLabel>When that limit is reached</SectionLabel>
        <RadioGroup
          value={state.overflow}
          onValueChange={(v) => patch({ overflow: v as TriggerOverflow })}
          aria-label="Overflow behaviour"
        >
          <Grid cols={3} gap="cozy">
            {OVERFLOW_OPTIONS.map((o) => (
              <RadioCard
                key={o.id}
                value={o.id}
                selected={state.overflow === o.id}
                onSelect={(v) => patch({ overflow: v as TriggerOverflow })}
                icon={o.icon}
                title={o.label}
                description={
                  o.best.includes(definition.id)
                    ? `${o.description} Recommended here.`
                    : o.description
                }
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      {state.overflow === 'wait' && (
        <Stack maxWidth="form-field">
          <FormField
            label="Max queued deliveries"
            id="wizard-queue-depth"
            hint="Beyond this the oldest waiting delivery is dropped."
          >
            <Input
              id="wizard-queue-depth"
              mono
              inputMode="numeric"
              value={String(state.maxQueueDepth)}
              onChange={(e) => patch({ maxQueueDepth: Number(e.target.value) || 0 })}
              endAdornment={
                <Text as="span" size="caption" color="subtle">
                  waiting
                </Text>
              }
            />
          </FormField>
        </Stack>
      )}

      <Callout variant="info">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Icon name="route" size="sm" color="accent" label="" shrink />
          <Text size="caption" color="muted">
            <Text as="span" size="caption" weight="heavy">
              {`${parallelWord(state.maxParallel)}. `}
            </Text>
            {overflowDetail}
          </Text>
        </Stack>
      </Callout>
    </Stack>
  );
}
