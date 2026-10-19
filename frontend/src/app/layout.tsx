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

/** Next serves `app/icon.svg` here. */
const DEFAULT_FAVICON = '/icon.svg';

/** The theme's favicon entries by colour scheme, or none so `app/icon.svg` stands. */
function themeIcons(brand: IdentityTheme): Pick<Metadata, 'icons'> {
  const { favicon } = brand;
  if (!favicon?.dark && !favicon?.light) return {};
  // Browsers follow the system colour scheme, not `data-theme`. A missing mode gets the kora icon.
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
  // Opt out of prerendering: the theme is mounted at runtime, and a build would ship the default.
  await connection();
  const { brand, css, fontPreloads } = await getTheme();

  return (
    // suppressHydrationWarning: next-themes sets data-theme pre-hydration.
    // The font variables are on <html> so the :root rule in globals.css can read them.
    <html lang="en" className={`${inter.variable} ${geistMono.variable}`} suppressHydrationWarning>
      <head>
        {/* Theme tokens and @font-face rules, inline so the first paint has the
            brand. Their selectors out-rank the token sheet, so stylesheet order
            does not matter. */}
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
