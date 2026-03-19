import * as React from 'react';
import { cn } from '../../utils';
import { Heading, Text } from '../typography/typography';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface SectionProps {
  /**
   * Optional visible heading — rendered as an `<h2>` using the `'section'`
   * Heading variant.
   */
  heading?: React.ReactNode;
  /**
   * Optional subheading / description rendered below the heading.
   * Uses a muted supporting text style.
   */
  description?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Section
 *
 * A content grouping block for use inside `PageLayout`'s main area (or any
 * scrollable column). Provides consistent horizontal padding, vertical rhythm,
 * and an optional heading + description header.
 *
 * Stack multiple `Section` blocks vertically to build a structured page
 * without introducing ad-hoc spacing or padding overrides.
 *
 * ```tsx
 * // Section with heading and description
 * <Section heading="Active agents" description="Agents currently running in this workspace.">
 *   <AgentTable />
 * </Section>
 *
 * // Heading only
 * <Section heading="Overview">
 *   <StatsGrid />
 * </Section>
 *
 * // No heading — consistent padding only
 * <Section>
 *   <EmptyState />
 * </Section>
 * ```
 */
const Section = React.forwardRef<HTMLElement, SectionProps & React.HTMLAttributes<HTMLElement>>(
  ({ heading, description, children, className, ...props }, ref) => (
    <section ref={ref} className={cn('px-6 py-8', className)} {...props}>
      {(heading || description) && (
        <div className="mb-6">
          {heading && (
            <Heading variant="section">
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
  ),
);

Section.displayName = 'Section';

export { Section };
