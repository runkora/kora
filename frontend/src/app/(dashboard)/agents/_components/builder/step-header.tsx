'use client';

/** @tier 3 — agents-scoped. Shared step heading: icon tile, title, purpose statement. */

import { Heading, Icon, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';

export function StepHeader({
  icon,
  title,
  description,
}: {
  icon: IconName;
  title: string;
  description: string;
}) {
  return (
    <Stack direction="horizontal" gap="cozy" align="start">
      <Icon name={icon} size="md" color="accent" container containerVariant="accent" label="" />
      <Stack gap="tight">
        <Heading variant="subsection">{title}</Heading>
        <Text size="supporting" color="muted">
          {description}
        </Text>
      </Stack>
    </Stack>
  );
}
