import 'server-only';

import { headers } from 'next/headers';
import type { Metadata, Viewport } from 'next';
import { Geist, Geist_Mono } from 'next/font/google';
import { ThemeProvider } from 'next-themes';
import { TooltipProvider } from '@orbit/terra';
import { getDevEnvPrefix, PrefixProvider } from '@orbit/lib/routing';
import './globals.css';

const geistSans = Geist({
  variable: '--font-geist-sans',
  subsets: ['latin'],
});

const geistMono = Geist_Mono({
  variable: '--font-geist-mono',
  subsets: ['latin'],
});

export const metadata: Metadata = {
  title: 'Orbit',
  description: 'Control plane for autonomous AI agents.',
};

// Note: we deliberately do not set `maximumScale` or `userScalable: false` —
// disabling pinch-zoom is a WCAG 1.4.4 / 1.4.10 accessibility violation.
export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
};

export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  // A dev environment's app-ingress gateway strips its /{env-id} prefix
  // before this app ever sees the request, recording what it stripped in
  // X-Forwarded-Prefix (devenv-prefix.ts). Reading it here, once, at the
  // root, and handing it down via PrefixProvider is what lets client code
  // (AppLink, usePrefixedRouter, api-routes.ts's fetch targets) keep every
  // internally-constructed URL inside the same environment instead of
  // dropping to the unprefixed root on the first client-side navigation.
  const prefix = getDevEnvPrefix(await headers());

  return (
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased bg-background text-foreground`}
      >
        <ThemeProvider attribute="data-theme" defaultTheme="system" enableSystem>
          <TooltipProvider>
            <PrefixProvider prefix={prefix}>{children}</PrefixProvider>
          </TooltipProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
