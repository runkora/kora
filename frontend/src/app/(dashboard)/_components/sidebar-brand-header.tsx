import { BrandLogo, Stack } from '@orbit/hades';
import { defaultTheme } from '@orbit/theme/default';
import type { ReactNode } from 'react';

/**
 * Brand header shown in the sidebar header slot on desktop and reused inside
 * the mobile drawer. `action` renders an optional trailing control. The mobile
 * drawer passes its close button here so the brand markup lives in one place.
 */
export function SidebarBrandHeader({ action }: { action?: ReactNode } = {}) {
  const lockup = (
    <BrandLogo
      dark={defaultTheme.logo.dark}
      light={defaultTheme.logo.light}
      size={30}
      title={defaultTheme.productName}
    />
  );

  // height="full" fills the h-14 sidebarHeader slot in PageLayout, which matches
  // the page header height so both bottom borders align on the same pixel row.
  // The desktop header centres the lockup; the drawer keeps it left beside the close button.
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify={action ? 'between' : 'center'}
      height="full"
      paddingInline="comfortable"
    >
      {lockup}
      {action}
    </Stack>
  );
}
