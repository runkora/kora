import 'server-only';

import type { Metadata, Viewport } from 'next';
import { Geist_Mono, Inter } from 'next/font/google';
import { ThemeProvider } from 'next-themes';
import { TooltipProvider } from '@clear-route/hades';
import { defaultTheme } from '@orbit/theme/default';
import './globals.css';

// Inter is a variable font; the optical size axis tightens it automatically at heading sizes.
const inter = Inter({
  variable: '--font-inter',
  subsets: ['latin'],
  axes: ['opsz'],
});

const geistMono = Geist_Mono({
  variable: '--font-geist-mono',
  subsets: ['latin'],
});

export const metadata: Metadata = {
  title: defaultTheme.productName,
  description: 'Control plane for autonomous AI agents.',
};

// Note: we deliberately do not set `maximumScale` or `userScalable: false` —
// disabling pinch-zoom is a WCAG 1.4.4 / 1.4.10 accessibility violation.
export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration.
    // The font variables live on <html> so the :root font slot defaults in
    // globals.css can reference them.
    <html lang="en" className={`${inter.variable} ${geistMono.variable}`} suppressHydrationWarning>
      <body className="antialiased bg-background text-foreground">
        <ThemeProvider attribute="data-theme" defaultTheme="system" enableSystem>
          <TooltipProvider>{children}</TooltipProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
