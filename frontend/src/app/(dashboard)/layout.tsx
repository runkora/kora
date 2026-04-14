import 'server-only';

import { PageLayout } from '@orbit/terra';
import { DashboardHeader, DashboardSidebar, PermissionsProvider } from './_components';

/**
 * Dashboard shell layout — sidebar + header + scrollable main.
 *
 * Covers all routes under (dashboard)/: the root dashboard page, agents,
 * events, credentials, policy, identity, storage, and settings.
 *
 * `DashboardSidebar` is a client component (needs usePathname for active
 * state). `DashboardHeader` is a server component. Both are passed as slots
 * to Terra's `PageLayout` which handles the fixed-chrome, scrollable-main
 * structure at h-screen.
 *
 * `PermissionsProvider` fetches the principal's scope set from the BFF session
 * and provides it to all client components via React context (ADR-0037).
 */
export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <PermissionsProvider>
      <PageLayout sidebar={<DashboardSidebar />} header={<DashboardHeader />}>
        {children}
      </PageLayout>
    </PermissionsProvider>
  );
}
