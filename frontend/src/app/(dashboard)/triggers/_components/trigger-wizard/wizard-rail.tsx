'use client';

/** @tier 3 — triggers-scoped. The wizard's left step rail: progress dots,
 * titles, and the note that says what a trigger is accountable to. */

import {
  Button,
  Card,
  CardContent,
  Eyebrow,
  Icon,
  StepConnector,
  StepDot,
  Stack,
  Text,
} from '@orbit/terra';
import type { KindStep } from './types';

export interface WizardRailProps {
  steps: KindStep[];
  current: number;
  /** Highest step reached, so a completed step can be revisited. */
  maxVisited: number;
  onGoTo: (index: number) => void;
}

export function WizardRail({ steps, current, maxVisited, onGoTo }: WizardRailProps) {
  return (
    <Stack
      minWidth="compact"
      maxWidth="compact"
      shrink={false}
      paddingBlock="relaxed"
      paddingInline="comfortable"
      overflowY="auto"
    >
      <Stack paddingInline="snug" paddingBottom="comfortable">
        <Eyebrow>New trigger</Eyebrow>
      </Stack>

      <Stack>
        {steps.map((s, i) => {
          const done = i < current;
          const isCurrent = i === current;
          return (
            <Button
              key={s.id}
              variant="ghost"
              size="tile"
              fullWidth
              disabled={i > maxVisited}
              onClick={() => onGoTo(i)}
              aria-current={isCurrent ? 'step' : undefined}
            >
              <Stack direction="horizontal" gap="cozy" paddingInline="snug">
                <Stack align="center" gap="none" shrink={false}>
                  <StepDot size="lg" state={done ? 'complete' : isCurrent ? 'current' : 'pending'}>
                    {done ? <Icon name="check" size="xs" /> : i + 1}
                  </StepDot>
                  {i < steps.length - 1 && <StepConnector complete={done} />}
                </Stack>
                {/* textAlign start: a button's UA default is centered text,
                    which centres short titles while wide subtitles fill. */}
                <Stack gap="tight" paddingBottom="relaxed" paddingTop="tight" textAlign="start">
                  <Text
                    as="span"
                    size="supporting"
                    weight="heavy"
                    color={isCurrent || done ? 'foreground' : 'subtle'}
                  >
                    {s.railTitle}
                  </Text>
                  <Text as="span" size="micro" color="subtle">
                    {s.railSub}
                  </Text>
                </Stack>
              </Stack>
            </Button>
          );
        })}
      </Stack>

      <Stack grow />

      <Card subtle>
        <CardContent padded compact>
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" gap="snug">
              <Icon name="info" size="xs" color="accent" label="" />
              <Text as="span" size="caption" weight="heavy" color="muted">
                Policy-checked
              </Text>
            </Stack>
            <Text size="micro" color="subtle">
              A trigger starts work as the agent it targets. Every run it starts is checked against
              that agent&apos;s grants before anything executes.
            </Text>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
