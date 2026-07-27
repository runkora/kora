/** @tier 3 — triggers-scoped. Type picker that opens the New trigger flow. */

'use client';

import { Badge, Grid, RadioCard, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import type { TriggerKind } from '../_types';

/** A trigger kind that can be created today. */
interface AvailableKind {
  kind: TriggerKind;
  icon: IconName;
  title: string;
  description: string;
}

/**
 * A kind the model accommodates but that is not built.
 *
 * These are shown, disabled, rather than hidden. It tells an admin where the
 * product is going without promising a date, and it keeps the design honest:
 * if adding one of these would require reworking the delivery model, the model
 * is wrong now rather than later.
 */
interface PlannedKind {
  icon: IconName;
  title: string;
  description: string;
}

const AVAILABLE: AvailableKind[] = [
  {
    kind: 'webhook',
    icon: 'zap',
    title: 'Webhook',
    description: 'Start a run when an external system posts an event.',
  },
  {
    kind: 'scheduled',
    icon: 'clock',
    title: 'Schedule',
    description: 'Start a run on a recurring schedule, in a timezone you choose.',
  },
  {
    kind: 'manual',
    icon: 'play',
    title: 'Manual',
    description: 'Publish a small input form and an API path to start a run on demand.',
  },
];

const PLANNED: PlannedKind[] = [
  {
    icon: 'activity',
    title: 'Control-plane event',
    description: 'When something happens inside Orbit: a run finishes, a budget is crossed.',
  },
  {
    icon: 'route',
    title: 'Chained run',
    description: "When another agent's run reaches a given outcome.",
  },
  {
    icon: 'send',
    title: 'Chat command',
    description: 'When someone invokes the agent from Slack or Teams.',
  },
  {
    icon: 'layers',
    title: 'Queue or stream',
    description: 'When a message lands on a subscribed queue or topic.',
  },
  {
    icon: 'database',
    title: 'Object storage',
    description: 'When a file appears or changes in a watched bucket.',
  },
  {
    icon: 'bell',
    title: 'Monitor or alert',
    description: 'When an external monitoring system fires an alert.',
  },
];

interface TriggerKindPickerProps {
  onSelect: (kind: TriggerKind) => void;
}

/**
 * TriggerKindPicker is the first step of creating a trigger: choose how it
 * starts.
 *
 * Triggers are the platform's front door for starting work, so the choice on
 * this screen is "what wakes this up", never "what does it run". The target is
 * chosen later and is a separate concern.
 */
export function TriggerKindPicker({ onSelect }: TriggerKindPickerProps) {
  return (
    <Stack gap="relaxed">
      <Stack gap="snug">
        <Text weight="heavy">Available now</Text>
        <Grid cols={3} gap="comfortable">
          {AVAILABLE.map((k) => (
            <RadioCard
              key={k.kind}
              value={k.kind}
              selected={false}
              onSelect={(v) => onSelect(v as TriggerKind)}
              icon={k.icon}
              title={k.title}
              description={k.description}
            />
          ))}
        </Grid>
      </Stack>

      <Stack gap="snug">
        <Stack direction="horizontal" gap="snug" align="center">
          <Text weight="heavy">Planned</Text>
          <Badge variant="outline">Soon</Badge>
        </Stack>
        <Text size="supporting" color="muted">
          Each is another way for something to start. Nothing downstream changes, which is the test
          of whether this design is right.
        </Text>
        <Grid cols={3} gap="comfortable">
          {PLANNED.map((k) => (
            <RadioCard
              key={k.title}
              value={k.title}
              selected={false}
              disabled
              onSelect={() => undefined}
              icon={k.icon}
              title={k.title}
              description={k.description}
            />
          ))}
        </Grid>
      </Stack>
    </Stack>
  );
}
