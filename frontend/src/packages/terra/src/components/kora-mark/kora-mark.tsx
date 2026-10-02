import * as React from 'react';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. The Kora brand mark. */

export interface KoraMarkProps extends Omit<React.SVGAttributes<SVGSVGElement>, 'children'> {
  /** Height in pixels. The width follows the mark's proportions. Defaults to 20. */
  size?: number;
  /** Accessible name. Without it the mark is hidden from assistive technology. */
  title?: string;
}

/**
 * KoraMark
 *
 * One circle cut in half: the upper half in the text colour, the lower half
 * moved right, drawn slightly larger, in the accent.
 *
 * ```tsx
 * <KoraMark size={24} />
 * ```
 */
const KoraMark = React.forwardRef<SVGSVGElement, KoraMarkProps>(
  ({ size = 20, title, className, ...props }, ref) => {
    const id = React.useId();
    const upper = `${id}-upper`;
    const lower = `${id}-lower`;
    return (
      <svg
        ref={ref}
        data-slot="kora-mark"
        viewBox="0 0 172 151"
        height={size}
        width={(size * 172) / 151}
        role={title ? 'img' : undefined}
        aria-hidden={title ? undefined : true}
        className={cn('shrink-0', className)}
        {...props}
      >
        {title && <title>{title}</title>}
        <defs>
          <clipPath id={upper}>
            <rect x="0" y="0" width="150" height="72" />
          </clipPath>
          <clipPath id={lower}>
            <rect x="19" y="78" width="152" height="73" />
          </clipPath>
        </defs>
        <circle cx="75" cy="75" r="75" fill="var(--color-foreground)" clipPath={`url(#${upper})`} />
        <circle
          cx="95"
          cy="75"
          r="76"
          fill="var(--color-border-accent)"
          clipPath={`url(#${lower})`}
        />
      </svg>
    );
  },
);
KoraMark.displayName = 'KoraMark';

export { KoraMark };
