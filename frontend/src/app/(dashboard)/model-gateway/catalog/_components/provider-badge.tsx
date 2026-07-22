import { Badge } from '@orbit/terra';
import { providerLabel, providerBadgeVariant } from '../_utils/catalog-constants';

interface ProviderBadgeProps {
  provider: string;
}

export function ProviderBadge({ provider }: ProviderBadgeProps) {
  return (
    <Badge variant={providerBadgeVariant(provider)} size="sm">
      {providerLabel(provider)}
    </Badge>
  );
}
