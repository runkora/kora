'use client';

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
import { FIELD_IDS, LIMITS, overflowSentence, parallelWord } from './wizard-state';
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

export function StepTarget({ state, patch, context, definition, fieldError }: StepComponentProps) {
  const { agents } = context;

  return (
    <Stack gap="relaxed">
      <FormField
        label="Agent to run"
        id={FIELD_IDS.agent}
        hint="Runs start as this agent. Every run is checked against its grants before anything executes."
        error={fieldError(FIELD_IDS.agent)}
        required
      >
        <Stack gap="snug">
          {agents.length === 0 && (
            <Callout variant="warning">
              No agents are available to bind. Create an autonomous agent first.
            </Callout>
          )}
          <RadioGroup
            id={FIELD_IDS.agent}
            value={state.agentTypeId}
            onValueChange={(v) => patch({ agentTypeId: v })}
            aria-label="Agent to run"
          >
            <Stack gap="snug">
              {agents.map((a) => (
                <Card
                  key={a.id}
                  interactive
                  selected={state.agentTypeId === a.id}
                  onClick={() => patch({ agentTypeId: a.id })}
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
                      <RadioGroupItem value={a.id} aria-label={a.name?.trim() || a.slug} />
                    </Stack>
                  </CardContent>
                </Card>
              ))}
            </Stack>
          </RadioGroup>
        </Stack>
      </FormField>

      <Stack gap="cozy">
        <SectionLabel>Overlapping runs</SectionLabel>

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
                <Text
                  as="span"
                  size="supporting"
                  mono
                  weight="heavy"
                  tabular
                  id={FIELD_IDS.parallel}
                >
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

        <RadioGroup
          value={state.overflow}
          onValueChange={(v) => patch({ overflow: v as TriggerOverflow })}
          aria-label="When that limit is reached"
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

        <Text size="caption" color="subtle">
          A trigger can fire faster than the agent finishes. {overflowSentence(state)}.
        </Text>
      </Stack>

      {state.overflow === 'wait' && (
        <Stack maxWidth="search-field">
          <FormField
            label="Max queued deliveries"
            id={FIELD_IDS.queueDepth}
            hint="Beyond this the oldest waiting delivery is dropped."
            error={fieldError(FIELD_IDS.queueDepth)}
          >
            <Input
              id={FIELD_IDS.queueDepth}
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
    </Stack>
  );
}
