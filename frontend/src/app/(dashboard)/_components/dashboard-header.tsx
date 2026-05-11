'use client';

import { usePathname } from 'next/navigation';
import { Button, Icon, Indicator, Separator, Stack, Text } from '@orbit/terra';

const SECTION_LABELS: Record<string, string> = {
  agents: 'Agents',
  policy: 'Policy',
  identity: 'Identity',
  'model-gateway': 'Model Gateway',
  chat: 'Chat',
};

function useSectionLabel(): string {
  const pathname = usePathname();
  const segment = pathname.split('/').filter(Boolean)[0] ?? '';
  return SECTION_LABELS[segment] ?? 'Dashboard';
}

export function DashboardHeader() {
  const section = useSectionLabel();

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      height="full"
      paddingInline="comfortable"
    >
      <Stack
        direction="horizontal"
        align="center"
        gap="snug"
        role="navigation"
        aria-label="Breadcrumb"
      >
        <Text size="supporting" color="muted">
          Orbit
        </Text>
        <Icon name="chevron-right" size="2xs" color="muted" label="" />
        <Text size="supporting" weight="emphasis">
          {section}
        </Text>
      </Stack>

      <Stack direction="horizontal" align="center" gap="snug">
        <Button variant="ghost" size="icon" aria-label="Refresh">
          <Icon name="refresh-cw" />
        </Button>
        <Indicator status="error">
          <Button variant="ghost" size="icon" aria-label="Notifications">
            <Icon name="bell" />
          </Button>
        </Indicator>
        <Separator orientation="vertical" span="narrow" />
        <Button variant="default" size="sm">
          <Icon name="bot" />
          Deploy Agent
        </Button>
      </Stack>
    </Stack>
  );
}
