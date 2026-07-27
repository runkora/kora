import * as React from 'react';
import { cn } from '../../utils';

export type SectionLabelProps = React.HTMLAttributes<HTMLDivElement>;

/**
 * SectionLabel
 *
 * A short all-caps heading that separates blocks inside a dense panel. Tracked
 * and dimmed so it organises without competing with the content it introduces —
 * at this weight it reads as structure rather than as another thing to read.
 *
 * Use `Heading` for headings that carry meaning on their own; this is for
 * labelling regions of a form or detail panel.
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
      className={cn(
        'font-sans text-[10px] font-semibold uppercase tracking-[0.13em] text-subtle',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  ),
);
SectionLabel.displayName = 'SectionLabel';

export { SectionLabel };
