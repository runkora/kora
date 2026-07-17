'use client';

/** @tier 3 — agents-scoped. The builder's left step rail: progress dots, titles, identity-first note. */

import {
  Button,
  Card,
  CardContent,
  Eyebrow,
  Icon,
  Stack,
  StepConnector,
  StepDot,
  Text,
} from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { BUILDER_STEPS } from '../../_hooks/use-agent-builder';

export function StepRail() {
  const { step, maxVisited, goTo } = useAgentBuilderContext();

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
        <Eyebrow>Create agent</Eyebrow>
      </Stack>

      <Stack>
        {BUILDER_STEPS.map((s, i) => {
          const done = i < step;
          const current = i === step;
          const reachable = i <= maxVisited;
          return (
            <Button
              key={s.key}
              variant="ghost"
              size="tile"
              fullWidth
              disabled={!reachable}
              onClick={() => goTo(i)}
              aria-current={current ? 'step' : undefined}
            >
              <Stack direction="horizontal" gap="cozy" paddingInline="snug">
                <Stack align="center" gap="none" shrink={false}>
                  <StepDot size="lg" state={done ? 'complete' : current ? 'current' : 'pending'}>
                    {done ? <Icon name="check" size="xs" /> : i + 1}
                  </StepDot>
                  {i < BUILDER_STEPS.length - 1 && <StepConnector complete={done} />}
                </Stack>
                <Stack gap="tight" paddingBottom="relaxed" paddingTop="tight">
                  <Text
                    as="span"
                    size="supporting"
                    weight="heavy"
                    color={current || done ? 'foreground' : 'subtle'}
                  >
                    {s.title}
                  </Text>
                  <Text as="span" size="micro" color="subtle">
                    {s.sub}
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
                Identity-first
              </Text>
            </Stack>
            <Text size="micro" color="subtle">
              Every agent gets a verified identity before it can touch a single tool. Access is
              checked before execution, never after.
            </Text>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
