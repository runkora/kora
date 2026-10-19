import 'server-only';

import type { Metadata, Viewport } from 'next';
import { Geist_Mono, Inter } from 'next/font/google';
import { connection } from 'next/server';
import { ThemeProvider } from 'next-themes';
import { TooltipProvider } from '@runkora/hades';
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
 * `media` query, the kora default standing in for a mode the theme omits. Without a theme favicon the key is left out and Next's
 * `app/icon.svg` file convention supplies the kora default, as before.
 */
/** Next serves `app/icon.svg` here. */
const DEFAULT_FAVICON = '/icon.svg';

function themeIcons(brand: IdentityTheme): Pick<Metadata, 'icons'> {
  const { favicon } = brand;
  if (!favicon?.dark && !favicon?.light) return {};
  // A mode the theme leaves out gets the kora default icon, as any missing
  // identity value does.
  return {
    icons: {
      icon: [
        {
          url: favicon.dark ?? DEFAULT_FAVICON,
          type: 'image/svg+xml',
          media: '(prefers-color-scheme: dark)',
        },
        {
          url: favicon.light ?? DEFAULT_FAVICON,
          type: 'image/svg+xml',
          media: '(prefers-color-scheme: light)',
        },
      ],
    },
  };
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
  // The theme is read from a document mounted at runtime, so no page may be
  // prerendered at build, where there is no document and the default would ship.
  await connection();
  const { brand, css, fontPreloads } = await getTheme();

  return (
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration.
    // The font variables are on <html> so the :root rule in globals.css can read them.
    <html lang="en" className={`${inter.variable} ${geistMono.variable}`} suppressHydrationWarning>
      <head>
        {/* The theme's token overrides and @font-face rules, inline so the first
            paint carries the brand. The selectors out-rank the token sheet's, so
            where Next places its stylesheet links does not matter. */}
        {css && <style data-theme-css="" dangerouslySetInnerHTML={{ __html: css }} />}
        {[...new Set(fontPreloads)].map((href) => (
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
