'use client';

import type { Provider } from '../_types/catalog';
import { PROVIDER_LABELS, PROVIDER_COLORS } from '../_utils/catalog-constants';

export function ProviderBadge({ provider }: { provider: Provider }) {
  return (
    <span
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${PROVIDER_COLORS[provider]}`}
    >
      {PROVIDER_LABELS[provider]}
    </span>
  );
}
