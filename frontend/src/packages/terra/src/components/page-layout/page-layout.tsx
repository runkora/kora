import * as React from 'react';
import { cn } from '../../utils';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface PageLayoutProps {
  /** Fixed-width left sidebar (optional — omit for full-width layouts). */
  sidebar?: React.ReactNode;
  /** Fixed top header bar (optional). */
  header?: React.ReactNode;
  /** Main scrollable content. */
  children: React.ReactNode;
  className?: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * PageLayout
 *
 * Top-level page shell that enforces the three-zone layout: a fixed sidebar,
 * a fixed header bar, and a scrollable main content area. Compose with
 * `Section` for internal content grouping.
 *
 * The layout is purely structural — no colour opinions beyond the background
 * token, so individual zones can be themed independently.
 *
 * Sidebar and header are both optional: omit the sidebar for full-width
 * canvas layouts; omit the header for minimal shells.
 *
 * ```tsx
 * // Full shell — sidebar + header + scrollable main
 * <PageLayout sidebar={<Sidebar />} header={<Header />}>
 *   <Section heading="Overview">…</Section>
 * </PageLayout>
 *
 * // No sidebar — full-width canvas (e.g. AI chat, doc editor)
 * <PageLayout header={<Header />}>
 *   <Section>…</Section>
 * </PageLayout>
 *
 * // Bare shell — just the scrollable main area
 * <PageLayout>
 *   <Section>…</Section>
 * </PageLayout>
 * ```
 */
const PageLayout = ({ sidebar, header, children, className }: PageLayoutProps) => (
  <div className={cn('flex h-screen w-full overflow-hidden bg-background', className)}>
    {sidebar && (
      <aside className="h-full w-60 shrink-0 flex flex-col border-r border-border-subtle bg-surface overflow-y-auto">
        {sidebar}
      </aside>
    )}
    <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
      {header && (
        <header className="shrink-0 h-14 flex items-center border-b border-border-subtle bg-surface px-6">
          {header}
        </header>
      )}
      <main className="flex-1 overflow-y-auto">{children}</main>
    </div>
  </div>
);

PageLayout.displayName = 'PageLayout';

export { PageLayout };
