import * as React from 'react';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. The Kora logo: the mark and the wordmark as one drawing. */

export interface KoraLogoProps extends Omit<React.SVGAttributes<SVGSVGElement>, 'children'> {
  /** Height in pixels, which is the height of the mark. The width follows. Defaults to 28. */
  size?: number;
  /** Accessible name. Without it the logo is hidden from assistive technology. */
  title?: string;
}

const WIDTH = 522.87;
const HEIGHT = 151;

/**
 * The wordmark is Archivo ExtraBold at 110% width with -0.05em tracking, drawn as
 * outlines at 96% of the brand kit's size (the kit sets the type size equal to the
 * mark's height), centred on the mark with 24 units between them. It is artwork, so it does not change with the UI font.
 */
const WORDMARK =
  'M205.07 123.6V19.2H229.98V78.38L259.22 47.71H288.74L259.94 77.8L289.74 123.6H261.09L242.37 93.5L229.98 104.01V123.6ZM328.77 125.32Q315.09 125.32 305.15 120.93Q295.22 116.54 289.82 107.76Q284.42 98.97 284.42 85.58Q284.42 72.19 289.82 63.4Q295.22 54.62 305.15 50.3Q315.09 45.98 328.77 45.98Q342.45 45.98 352.38 50.3Q362.32 54.62 367.65 63.4Q372.98 72.19 372.98 85.58Q372.98 98.97 367.65 107.76Q362.32 116.54 352.38 120.93Q342.45 125.32 328.77 125.32ZM328.77 107.76Q335.25 107.76 339.5 105.38Q343.74 103 345.76 98.54Q347.78 94.08 347.78 87.88V83.42Q347.78 77.23 345.76 72.69Q343.74 68.16 339.5 65.78Q335.25 63.4 328.77 63.4Q322.14 63.4 317.97 65.78Q313.79 68.16 311.78 72.69Q309.76 77.23 309.76 83.42V87.88Q309.76 94.08 311.78 98.54Q313.79 103 317.97 105.38Q322.14 107.76 328.77 107.76ZM380.6 123.6V47.71H400.9L402.63 59.8H403.78Q405.65 55.63 408.61 52.46Q411.56 49.29 415.52 47.56Q419.48 45.84 424.23 45.84Q426.97 45.84 429.27 46.34Q431.57 46.84 433.16 47.42V68.16H423.65Q418.9 68.16 415.45 69.6Q411.99 71.04 409.76 73.63Q407.53 76.22 406.52 79.82Q405.51 83.42 405.51 87.88V123.6ZM458.93 125.32Q453.32 125.32 448.13 124.32Q442.95 123.31 438.92 120.93Q434.89 118.56 432.58 114.31Q430.28 110.06 430.28 103.58Q430.28 94.94 434.38 89.76Q438.49 84.57 445.97 81.84Q453.46 79.1 463.97 78.16Q474.49 77.23 487.3 77.23V73.34Q487.3 69.6 485.65 67.22Q483.99 64.84 480.82 63.69Q477.65 62.54 472.47 62.54Q468.44 62.54 465.05 63.33Q461.67 64.12 459.73 65.71Q457.78 67.29 457.78 69.74V70.89H433.16Q433.01 70.32 433.01 69.81Q433.01 69.31 433.01 68.59Q433.01 61.82 437.69 56.71Q442.37 51.6 451.3 48.79Q460.23 45.98 473.19 45.98Q485.14 45.98 493.85 48.5Q502.57 51.02 507.32 56.42Q512.07 61.82 512.07 70.75V102.28Q512.07 104.88 513.37 106.32Q514.66 107.76 516.97 107.76H522.87V122.73Q521.14 123.45 517.33 124.39Q513.51 125.32 508.61 125.32Q502.85 125.32 498.97 124.03Q495.08 122.73 492.77 120.36Q490.47 117.98 489.61 114.96H488.6Q485.29 118.12 480.89 120.43Q476.5 122.73 471.1 124.03Q465.7 125.32 458.93 125.32ZM467.29 108.76Q470.6 108.76 474.13 107.9Q477.65 107.04 480.68 105.31Q483.7 103.58 485.5 100.99Q487.3 98.4 487.3 95.23V90.33Q476.79 90.33 469.59 91.48Q462.39 92.64 458.79 95.08Q455.19 97.53 455.19 101.28Q455.19 104.16 456.85 105.81Q458.5 107.47 461.24 108.12Q463.97 108.76 467.29 108.76Z';

/**
 * KoraLogo
 *
 * The circle cut in half, in the accent below and the text colour above, with
 * "kora" beside it in the text colour.
 *
 * ```tsx
 * <KoraLogo size=28 title="kora" />
 * ```
 */
const KoraLogo = React.forwardRef<SVGSVGElement, KoraLogoProps>(
  ({ size = 28, title, className, ...props }, ref) => {
    const id = React.useId();
    const upper = `${id}-upper`;
    const lower = `${id}-lower`;
    return (
      <svg
        ref={ref}
        data-slot="kora-logo"
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        height={size}
        width={(size * WIDTH) / HEIGHT}
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
        <path d={WORDMARK} fill="var(--color-foreground)" />
      </svg>
    );
  },
);
KoraLogo.displayName = 'KoraLogo';

export { KoraLogo };
