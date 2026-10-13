/** @tier 1 — Zero Orbit domain knowledge. Branded loading animation. */
'use client';

import * as React from 'react';
import { cn } from '../../utils';

export interface OrbitalLoaderProps {
  /** Visual size. `md` (default) is 44 × 44 px; `sm` is 32 × 32 px. */
  size?: 'sm' | 'md';
  className?: string;
}

const OrbitalLoader = React.forwardRef<HTMLDivElement, OrbitalLoaderProps>(
  ({ size = 'md', className }, ref) => {
    const isSm = size === 'sm';
    return (
      <div
        ref={ref}
        data-slot="orbital-loader"
        aria-label="Loading"
        role="status"
        className={cn(
          'relative inline-flex items-center justify-center shrink-0',
          isSm ? 'w-8 h-8' : 'w-11 h-11',
          className,
        )}
      >
        {/* Core pulsing dot */}
        <div
          className={cn(
            'rounded-full bg-accent relative z-10',
            isSm
              ? 'w-[5px] h-[5px] shadow-[0_0_6px_var(--color-accent)]'
              : 'w-[6px] h-[6px] shadow-[0_0_10px_color-mix(in_oklch,var(--color-accent)_60%,transparent)]',
            'animate-[orbital-core-pulse_2.4s_ease-in-out_infinite]',
          )}
        />
        {/* Ring 1 */}
        <div
          className={cn(
            'absolute rounded-full border border-accent/35 top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2',
            isSm ? 'w-4 h-4' : 'w-5 h-5',
          )}
        />
        {/* Ring 2 */}
        <div
          className={cn(
            'absolute rounded-full border top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2',
            isSm ? 'w-[26px] h-[26px] border-accent/18' : 'w-8 h-8 border-accent/20',
          )}
        />
        {/* Ring 3 (md only) */}
        {!isSm && (
          <div className="absolute rounded-full border border-accent/10 top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-11 h-11" />
        )}
        {/* Orbiting dot 1 */}
        <div
          className={cn(
            'absolute rounded-full bg-accent top-1/2 left-1/2',
            isSm
              ? 'w-[3.5px] h-[3.5px] -mt-[1.75px] -ml-[1.75px] animate-[orbital-dot-sm1_1.8s_linear_infinite]'
              : 'w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md1_1.8s_linear_infinite]',
          )}
        />
        {/* Orbiting dot 2 */}
        <div
          className={cn(
            'absolute rounded-full bg-accent top-1/2 left-1/2',
            isSm
              ? 'w-[3.5px] h-[3.5px] -mt-[1.75px] -ml-[1.75px] animate-[orbital-dot-sm2_3.2s_linear_infinite_reverse]'
              : 'w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md2_3.2s_linear_infinite_reverse]',
          )}
        />
        {/* Orbiting dot 3 (md only) */}
        {!isSm && (
          <div className="absolute rounded-full bg-accent top-1/2 left-1/2 w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md3_5.0s_linear_infinite]" />
        )}
      </div>
    );
  },
);
OrbitalLoader.displayName = 'OrbitalLoader';

export { OrbitalLoader };
