'use client';

import { usePathname } from 'next/navigation';
import { Breadcrumbs, Button, Icon, Indicator, Separator, Show, Stack } from '@orbit/terra';
import { useSidebarState } from './sidebar-state';

const SECTION_LABELS: Record<string, string> = {
  agents: 'Agents',
  policy: 'Policy',
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
  const { toggle, isOpen } = useSidebarState();

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      height="full"
      paddingInline="comfortable"
    >
      <Stack direction="horizontal" align="center" gap="snug">
        {/* Hamburger — only visible on mobile (hidden at md+) */}
        <Show below="md">
          <Button
            variant="ghost"
            size="icon"
            aria-label={isOpen ? 'Close navigation' : 'Open navigation'}
            aria-expanded={isOpen}
            onClick={toggle}
          >
            <Icon name="menu" />
          </Button>
        </Show>

        <Breadcrumbs items={[{ label: 'Orbit' }, { label: section }]} />
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
