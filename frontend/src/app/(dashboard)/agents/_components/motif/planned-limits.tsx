'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { Badge, Card, CardContent, Eyebrow, Icon, Separator, Stack, Text } from '@orbit/hades';
import type { IconName } from '@orbit/hades';

const PLANNED: { icon: IconName; title: string; desc: string }[] = [
  {
    icon: 'activity',
    title: 'Rate limit',
    desc: 'Cap requests per minute across a run.',
  },
  {
    icon: 'cpu',
    title: 'Max tokens per run',
    desc: 'Context + output ceiling for a single run.',
  },
  {
    icon: 'wrench',
    title: 'Max tool calls per run',
    desc: 'Runaway-loop protection.',
  },
  {
    icon: 'eye-off',
    title: 'Redact PII in prompts & logs',
    desc: 'Detected personal data is masked before it reaches the model or the audit trail.',
  },
  {
    icon: 'user-check',
    title: 'Require human approval for write actions',
    desc: 'Destructive tool calls pause for sign-off in the run inspector.',
  },
];

export function PlannedLimits() {
  return (
    <Card subtle>
      <CardContent flush>
        <Stack direction="horizontal" align="baseline" gap="snug" padding="cozy">
          <Eyebrow>Planned</Eyebrow>
          <Text as="span" size="micro" color="subtle">
            On the roadmap, not yet enforced
          </Text>
        </Stack>
        {PLANNED.map((p) => (
          <Stack key={p.title} gap="none">
            <Separator />
            <Stack direction="horizontal" align="center" gap="cozy" padding="cozy" disabled>
              <Icon
                name={p.icon}
                size="sm"
                color="subtle"
                container
                containerVariant="surface-subtle"
                label=""
              />
              <Stack grow gap="tight">
                <Text as="span" size="supporting" weight="heavy">
                  {p.title}
                </Text>
                <Text size="caption" color="muted">
                  {p.desc}
                </Text>
              </Stack>
              <Badge shape="pill" variant="outline" size="sm">
                Coming soon
              </Badge>
            </Stack>
          </Stack>
        ))}
      </CardContent>
    </Card>
  );
}
