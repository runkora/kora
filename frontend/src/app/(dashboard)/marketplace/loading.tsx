import { Skeleton, Stack } from '@orbit/terra';
import { MarketplaceResultsSkeleton } from './_components/marketplace-results-skeleton';

/**
 * The Marketplace page loads the Marketplace options and one result page of
 * Skills in one server pass, so the skeleton stands in for the whole surface:
 * the header, the toolbar, the Skills table, and the pagination bar.
 */
export default function MarketplaceLoading() {
  return (
    <Stack gap="comfortable">
      {/* Page header: title + explainer. */}
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>

      {/* Toolbar, Skills table, and pagination bar. */}
      <MarketplaceResultsSkeleton />
    </Stack>
  );
}
