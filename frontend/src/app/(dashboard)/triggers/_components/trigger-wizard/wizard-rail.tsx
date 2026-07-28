'use client';

/** @tier 3 — triggers-scoped. The wizard's left rail: what is being created,
 * and how far through it you are. Present in every phase, so the frame never
 * changes shape under the person using it. */

import { Button, Icon, StepConnector, StepDot, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';

export interface RailStep {
  id: string;
  title: string;
}

export interface WizardRailProps {
  steps: RailStep[];
  /** Index of the current step. Pass `steps.length` to show every step complete. */
  current: number;
  /** Highest step reached, so a completed step can be revisited. */
  maxVisited: number;
  onGoTo: (index: number) => void;
  /** The chosen kind's identity. Absent on the picker phase. */
  kindIcon?: IconName;
  kindTitle?: string;
  /** Returns to the picker. Absent once the trigger exists. */
  onChangeKind?: () => void;
}

export function WizardRail({
  steps,
  current,
  maxVisited,
  onGoTo,
  kindIcon,
  kindTitle,
  onChangeKind,
}: WizardRailProps) {
  return (
    <Stack
      minWidth="compact"
      maxWidth="compact"
      shrink={false}
      paddingBlock="loose"
      paddingInline="comfortable"
      overflowY="auto"
    >
      <Stack
        direction="horizontal"
        align="center"
        gap="snug"
        paddingInline="snug"
        paddingBottom="comfortable"
      >
        <Icon name={kindIcon ?? 'zap'} size="xs" color={kindTitle ? 'accent' : 'subtle'} label="" />
        <Text as="span" size="caption" weight="heavy" color={kindTitle ? 'foreground' : 'subtle'}>
          {kindTitle ?? 'Choose a kind'}
        </Text>
        <Stack grow />
        {onChangeKind && (
          <Button variant="ghost" size="xs" onClick={onChangeKind}>
            Change
          </Button>
        )}
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
              <Stack direction="horizontal" align="center" gap="cozy" paddingInline="snug">
                <Stack align="center" gap="none" shrink={false}>
                  <StepDot size="lg" state={done ? 'complete' : isCurrent ? 'current' : 'pending'}>
                    {done ? <Icon name="check" size="xs" /> : i + 1}
                  </StepDot>
                  {i < steps.length - 1 && <StepConnector complete={done} />}
                </Stack>
                <Stack paddingBottom="relaxed" paddingTop="tight">
                  <Text
                    as="span"
                    size="supporting"
                    weight="heavy"
                    color={isCurrent || done ? 'foreground' : 'subtle'}
                  >
                    {s.title}
                  </Text>
                </Stack>
              </Stack>
            </Button>
          );
        })}
      </Stack>
    </Stack>
  );
}
