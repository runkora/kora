import 'server-only';

import { Stack } from '@orbit/terra';

/**
 * Auth layout — centered, no chrome.
 *
 * Covers the login, callback, and error routes under (auth)/.
 * Vertically and horizontally centres its children against the full viewport;
 * the root layout already supplies <body> and global providers, so this layout
 * only adds the centering wrapper.
 */
export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack asChild align="center" justify="center">
      <main className="min-h-screen">{children}</main>
    </Stack>
  );
}
