import 'server-only';

import type { Metadata, Viewport } from 'next';
import { Archivo, DM_Mono } from 'next/font/google';
import { ThemeProvider } from 'next-themes';
import { TooltipProvider } from '@orbit/terra';
import './globals.css';

// Archivo is a variable font; the width axis lets headings run at 110%.
const archivo = Archivo({
  variable: '--font-archivo',
  subsets: ['latin'],
  axes: ['wdth'],
});

const dmMono = DM_Mono({
  variable: '--font-dm-mono',
  subsets: ['latin'],
  weight: ['400', '500'],
});

export const metadata: Metadata = {
  title: 'kora',
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
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${archivo.variable} ${dmMono.variable} antialiased bg-background text-foreground`}
      >
        <ThemeProvider attribute="data-theme" defaultTheme="system" enableSystem>
          <TooltipProvider>{children}</TooltipProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
