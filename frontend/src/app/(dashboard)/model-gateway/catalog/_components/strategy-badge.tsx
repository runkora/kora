'use client';

import type { RoutingStrategy } from '../_types/catalog';
import { STRATEGY_LABELS, STRATEGY_COLORS } from '../_utils/catalog-constants';

export function StrategyBadge({ strategy }: { strategy: RoutingStrategy }) {
  return (
    // eslint-disable-next-line local/no-raw-div
    <div
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${STRATEGY_COLORS[strategy]}`}
    >
      {STRATEGY_LABELS[strategy]}
    </div>
  );
}
