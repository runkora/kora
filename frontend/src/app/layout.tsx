import 'server-only';

import type { Metadata, Viewport } from 'next';
import { Geist_Mono, Inter } from 'next/font/google';
import { ThemeProvider } from 'next-themes';
import { TooltipProvider } from '@clear-route/hades';
import { getTheme } from '@orbit/server/theme';
import { BrandProvider } from '@orbit/theme/brand-provider';
import type { IdentityTheme } from '@orbit/theme/default';
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

/**
 * The favicon entries for the theme. Browsers pick a favicon by the system
 * colour scheme, not the page's `data-theme`, so both modes go out with a
 * `media` query. Without a theme favicon the key is left out and Next's
 * `app/icon.svg` file convention supplies the kora default, as before.
 */
function themeIcons(brand: IdentityTheme): Pick<Metadata, 'icons'> {
  const { favicon } = brand;
  if (!favicon?.dark && !favicon?.light) return {};
  if (favicon.dark && favicon.light) {
    return {
      icons: {
        icon: [
          { url: favicon.dark, type: 'image/svg+xml', media: '(prefers-color-scheme: dark)' },
          { url: favicon.light, type: 'image/svg+xml', media: '(prefers-color-scheme: light)' },
        ],
      },
    };
  }
  return { icons: { icon: [{ url: (favicon.dark ?? favicon.light)!, type: 'image/svg+xml' }] } };
}

export async function generateMetadata(): Promise<Metadata> {
  const { brand } = await getTheme();
  return {
    title: brand.productName,
    description: 'Control plane for autonomous AI agents.',
    ...themeIcons(brand),
  };
}

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
  // Loaded once per process; a theme that fails validation throws here, so no
  // page serves (white-labelling theme ADR, decision 4).
  const { brand, css, fontPreloads } = await getTheme();

  return (
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration.
    // The font variables live on <html> so the :root font slot defaults in
    // globals.css can reference them.
    <html lang="en" className={`${inter.variable} ${geistMono.variable}`} suppressHydrationWarning>
      <head>
        {/* The theme's token overrides and @font-face rules, inline so the first
            paint carries the brand. The selectors out-rank the token sheet's, so
            where Next places its stylesheet links does not matter. */}
        {css && <style data-theme-css="" dangerouslySetInnerHTML={{ __html: css }} />}
        {fontPreloads.map((href) => (
          <link
            key={href}
            rel="preload"
            href={href}
            as="font"
            type="font/woff2"
            crossOrigin="anonymous"
          />
        ))}
      </head>
      <body className="antialiased bg-background text-foreground">
        <ThemeProvider attribute="data-theme" defaultTheme="system" enableSystem>
          <BrandProvider brand={brand}>
            <TooltipProvider>{children}</TooltipProvider>
          </BrandProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
