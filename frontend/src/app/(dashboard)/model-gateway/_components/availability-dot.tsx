import { cn } from '@orbit/terra';
import type { ModelStatus } from '../_types';

interface AvailabilityDotProps {
  status: ModelStatus;
  className?: string;
}

export function AvailabilityDot({ status, className }: AvailabilityDotProps) {
  return (
    <div
      role="img"
      aria-label={status === 'available' ? 'Available' : 'Unavailable'}
      className={cn(
        'inline-block size-2.5 rounded-full shrink-0',
        status === 'available'
          ? 'bg-status-healthy shadow-[0_0_0_3px_color-mix(in_oklch,var(--color-status-healthy)_15%,transparent)]'
          : 'bg-status-error shadow-[0_0_0_3px_color-mix(in_oklch,var(--color-status-error)_15%,transparent)]',
        className,
      )}
    />
  );
}
