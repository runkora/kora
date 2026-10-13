'use client';

import { Badge } from '@orbit/hades';
import type { RoutingStrategy } from '../_types/catalog';
import { STRATEGY_LABELS, STRATEGY_VARIANTS } from '../_utils/catalog-constants';

export function StrategyBadge({ strategy }: { strategy: RoutingStrategy }) {
  return (
    <Badge size="sm" variant={STRATEGY_VARIANTS[strategy]}>
      {STRATEGY_LABELS[strategy]}
    </Badge>
  );
}
