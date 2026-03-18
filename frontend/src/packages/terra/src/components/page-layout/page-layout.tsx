import * as React from 'react';
import { cn } from '../../utils';
import { Heading, Text } from '../typography/typography';

/**
 * PageLayout
 *
 * Top-level page shell that enforces the three-zone layout: a fixed sidebar,
 * a fixed header bar, and a scrollable main content area. Composes with
 * `Section` for internal content grouping.
 *
 * The layout is purely structural — no colour opinions beyond the background
 * token so individual zones can be themed independently.
 *
 * ```tsx
 * <PageLayout sidebar={<Sidebar />} header={<Header />}>
 *   <Section heading="Overview">…</Section>
 * </PageLayout>
 * ```
 */
interface PageLayoutProps {
  /** Fixed-width left sidebar (optional — omit for full-width layouts). */
  sidebar?: React.ReactNode;
  /** Fixed top header bar (optional). */
  header?: React.ReactNode;
  /** Main scrollable content. */
  children: React.ReactNode;
  className?: string;
}

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

/**
 * Section
 *
 * Content grouping block within a page. Provides consistent horizontal
 * padding, a max-width container, and an optional heading. Stack multiple
 * sections inside a `PageLayout` main area.
 *
 * ```tsx
 * <Section heading="Active agents">
 *   <AgentTable />
 * </Section>
 * ```
 */
interface SectionProps {
  /** Optional visible heading — rendered as an `<h2>` via Heading variant="section". */
  heading?: React.ReactNode;
  /** Optional subheading / description below the heading. */
  description?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

const Section = ({ heading, description, children, className }: SectionProps) => (
  <section className={cn('px-6 py-8', className)}>
    {(heading || description) && (
      <div className="mb-6">
        {heading && (
          <Heading variant="section" className="text-lg font-semibold">
            {heading}
          </Heading>
        )}
        {description && (
          <Text size="supporting" color="muted" className="mt-1">
            {description}
          </Text>
        )}
      </div>
    )}
    {children}
  </section>
);

Section.displayName = 'Section';

export { PageLayout, Section };
export type { PageLayoutProps, SectionProps };
