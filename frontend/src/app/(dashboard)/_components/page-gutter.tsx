'use client';

/** @tier 3 — dashboard-shell-scoped. Promote to src/components/ if a second feature needs this. */
import { usePathname } from 'next/navigation';
import { Stack } from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing/routes';

/**
 * The one content gutter for every dashboard page. /chat fills the shell
 * edge to edge and owns its scroll, so it is the only route left unpadded.
 */
export function PageGutter({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  if (pathname === APP_ROUTES.chat) return <>{children}</>;
  return <Stack padding="comfortable">{children}</Stack>;
}
