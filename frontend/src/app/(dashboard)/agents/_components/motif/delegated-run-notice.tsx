'use client';

/** @tier 3 — agents-scoped motif: explains what running a delegated agent
 * means for the person about to run it (or reading its runs list). Speaks to
 * the individual only. Attribution, privacy, and budget are framed as THEIR
 * properties; platform governance is not this surface's story. Mirrors the
 * PlannedLimits row motif: subtle card, flush content, separator-divided rows. */

import { Card, CardContent, Eyebrow, Icon, Separator, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';

const NOTICE_ROWS: { icon: IconName; title: string; desc: string }[] = [
  {
    icon: 'user-check',
    title: 'Attributed to you',
    desc: 'Every run is yours: it acts with your access and carries your name.',
  },
  {
    icon: 'lock',
    title: 'Private to you',
    desc: 'Your runs, messages, and outputs are visible only to you.',
  },
  {
    icon: 'dollar-sign',
    title: 'Uses your budget',
    desc: 'Usage counts against your individual budget allocations.',
  },
];

export function DelegatedRunNotice() {
  return (
    <Card subtle>
      <CardContent flush>
        <Stack direction="horizontal" align="baseline" gap="snug" padding="cozy">
          <Eyebrow>Delegated agent</Eyebrow>
          <Text as="span" size="micro" color="subtle">
            Runs on behalf of the person who starts them
          </Text>
        </Stack>
        {NOTICE_ROWS.map((row) => (
          <Stack key={row.title} gap="none">
            <Separator />
            <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
              <Icon
                name={row.icon}
                size="sm"
                color="accent"
                container
                containerVariant="surface-subtle"
                label=""
              />
              <Stack grow gap="tight">
                <Text as="span" size="supporting" weight="heavy">
                  {row.title}
                </Text>
                <Text size="caption" color="muted">
                  {row.desc}
                </Text>
              </Stack>
            </Stack>
          </Stack>
        ))}
      </CardContent>
    </Card>
  );
}
