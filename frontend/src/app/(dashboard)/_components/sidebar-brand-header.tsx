import { Badge, Icon, Stack, Text } from '@orbit/terra';
import type { ReactNode } from 'react';

/**
 * Brand + version header shown in the sidebar header slot on desktop and reused
 * inside the mobile drawer. `action` renders an optional trailing control next
 * to the version badge — the mobile drawer passes its close button here so the
 * brand markup and version string live in exactly one place.
 */
export function SidebarBrandHeader({ action }: { action?: ReactNode } = {}) {
  return (
    // height="full" fills the h-14 sidebarHeader slot in PageLayout, which matches
    // the page header height so both bottom borders align on the same pixel row.
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      height="full"
      paddingInline="comfortable"
    >
      <Stack direction="horizontal" align="center" gap="snug">
        <Icon name="zap" size="sm" container containerVariant="accent" />
        <Text size="supporting" weight="strong">
          Orbit
        </Text>
      </Stack>
      <Stack direction="horizontal" align="center" gap="snug">
        <Badge variant="outline" size="sm">
          {process.env.NEXT_PUBLIC_ORBIT_VERSION ?? 'dev'}
        </Badge>
        {action}
      </Stack>
    </Stack>
  );
}
