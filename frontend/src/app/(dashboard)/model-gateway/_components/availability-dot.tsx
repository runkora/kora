import { StatusDot } from '@orbit/terra';
import type { ModelStatus } from '../_types';

interface AvailabilityDotProps {
  status: ModelStatus;
}

export function AvailabilityDot({ status }: AvailabilityDotProps) {
  return (
    <StatusDot
      status={status === 'available' ? 'healthy' : 'error'}
      glow
      role="img"
      aria-label={status === 'available' ? 'Available' : 'Unavailable'}
    />
  );
}
