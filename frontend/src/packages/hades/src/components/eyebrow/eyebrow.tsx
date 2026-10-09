import * as React from 'react';
import { cn } from '../../utils';

export type EyebrowProps = React.ComponentPropsWithoutRef<'span'>;

/**
 * Eyebrow
 *
 * A compact ALL-CAPS overline label that sits above a heading or opens a small
 * card section ("AUDIT", "PLANNED LIMITS"). Renders an inline `<span>` with the
 * exact treatment previously hand-composed as
 * `<Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">`:
 * 11 px, bold, uppercase, widened tracking, subtle colour.
 *
 * **Prefer `SectionLabel` in new code.** The two draw the same visual idea at
 * slightly different weights, and `SectionLabel` is the one the rest of Hades
 * (DataGrid headers, StatCard labels, TemplateField's catalog) is built on, so
 * its `sectionLabelVariants` recipe is what keeps overlines consistent. This
 * one stays for the inline `<span>` case and its existing callers.
 *
 * ```tsx
 * <Eyebrow>Audit</Eyebrow>
 * ```
 */
const Eyebrow = React.forwardRef<HTMLSpanElement, EyebrowProps>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    data-slot="eyebrow"
    className={cn(
      // Mirrors Text: font-sans + size="micro" + color="subtle" + weight="heavy" + transform="uppercase"
      'font-sans text-[11px] leading-none text-subtle font-bold uppercase tracking-widest',
      className,
    )}
    {...props}
  />
));

Eyebrow.displayName = 'Eyebrow';

export { Eyebrow };
