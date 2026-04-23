import { Badge } from '@orbit/terra';
import type { Provider } from '../_types/catalog';
import { PROVIDER_LABELS, PROVIDER_VARIANTS } from '../_utils/catalog-constants';

interface ProviderBadgeProps {
  provider: Provider;
}

export function ProviderBadge({ provider }: ProviderBadgeProps) {
  return (
    <Badge variant={PROVIDER_VARIANTS[provider]} size="sm">
      {PROVIDER_LABELS[provider]}
    </Badge>
  );
}
