'use client';

import { usePathname } from 'next/navigation';
import { Breadcrumbs, Button, Icon, Indicator, Separator, Stack } from '@orbit/terra';

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
      <Breadcrumbs items={[{ label: 'Orbit' }, { label: section }]} />

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
