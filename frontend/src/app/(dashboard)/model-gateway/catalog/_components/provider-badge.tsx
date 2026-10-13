import { Badge, Stack } from '@clear-route/hades';
import { providerLabel, providerTitle, providerBadgeVariant } from '../_utils/catalog-constants';

interface ProviderBadgeProps {
  provider: string;
}

// The Stack clips a name too long for its column; the title carries it in full.
export function ProviderBadge({ provider }: ProviderBadgeProps) {
  return (
    <Stack direction="horizontal" overflow="hidden" minWidth="none">
      <Badge
        variant={providerBadgeVariant(provider)}
        size="sm"
        mono
        title={providerTitle(provider)}
      >
        {providerLabel(provider)}
      </Badge>
    </Stack>
  );
}
