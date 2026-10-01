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
    <Stack asChild align="center" justify="center" padding="comfortable">
      <main
        className="min-h-screen"
        style={{
          // A faint accent glow at the top keeps the page from reading as flat black or flat chalk.
          background:
            'radial-gradient(60rem 28rem at 50% 0%, color-mix(in oklch, var(--color-accent) 9%, transparent), transparent)',
        }}
      >
        {children}
      </main>
    </Stack>
  );
}
