import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. The overline that labels a region of dense UI. */

/**
 * The one overline treatment in Terra: 10px, semibold, uppercase, tracked to
 * 0.13em, subtle colour. Exported so surfaces that cannot render a
 * `<SectionLabel>` element — a grid header cell, a stat card's label span —
 * still draw the same overline instead of hand-tuning a fourth tracking value.
 *
 * Reuse this rather than restating the classes. If the treatment changes it
 * should change everywhere at once, which is the point of it being one recipe.
 */
const sectionLabelVariants = cva([
  'font-sans',
  'text-[10px]',
  'font-semibold',
  'uppercase',
  'tracking-[0.13em]',
  'text-subtle',
]);

export type SectionLabelProps = React.HTMLAttributes<HTMLDivElement>;

/**
 * SectionLabel
 *
 * A short all-caps heading that separates blocks inside a dense panel. Tracked
 * and dimmed so it organises without competing with the content it introduces —
 * at this weight it reads as structure rather than as another thing to read.
 *
 * Use `Heading` for headings that carry meaning on their own; this is for
 * labelling regions of a form or detail panel. Prefer this over `Eyebrow` in
 * new code: both draw an overline, and this is the one the rest of Terra
 * (DataGrid headers, StatCard labels) is built on.
 *
 * ```tsx
 * <SectionLabel>Endpoint</SectionLabel>
 * ```
 */
const SectionLabel = React.forwardRef<HTMLDivElement, SectionLabelProps>(
  ({ className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="section-label"
      className={cn(sectionLabelVariants(), className)}
      {...props}
    >
      {children}
    </div>
  ),
);
SectionLabel.displayName = 'SectionLabel';

export { SectionLabel, sectionLabelVariants };
