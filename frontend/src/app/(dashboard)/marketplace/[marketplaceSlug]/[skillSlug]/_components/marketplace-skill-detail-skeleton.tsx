/** @tier 3 — marketplace/[marketplaceSlug]/[skillSlug]-scoped. Placeholder for the server-loaded Skill page. */

import {
  Card,
  CardContent,
  CardHeader,
  SidebarLayout,
  SidebarLayoutMain,
  SidebarLayoutSidebar,
  Skeleton,
  Stack,
} from '@orbit/hades';

/**
 * Stands in for the Skill details while the Server Component loads:
 * the identity header, files, and Source sidebar.
 */
export function MarketplaceSkillDetailSkeleton() {
  return (
    <Stack role="status" aria-label="Loading the Skill">
      <Stack gap="relaxed">
        {/* Identity header: title, Marketplace, and description. */}
        <Stack gap="tight">
          <Skeleton shape="line" width="third" size="lg" />
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>

        {/* Body: files left, Source card right. */}
        <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
          <SidebarLayoutMain>
            <Stack gap="comfortable">
              {/* "Contents" heading and resource count. */}
              <Stack direction="horizontal" align="center" justify="between">
                <Skeleton shape="line" width="quarter" size="sm" />
                <Skeleton shape="line" width="quarter" size="xs" />
              </Stack>

              {/* Files: the package file and its resources. */}
              {Array.from({ length: 2 }).map((_, i) => (
                <Card key={i} column>
                  <CardHeader>
                    <Skeleton shape="line" width="third" size="sm" />
                  </CardHeader>
                  <CardContent>
                    <Skeleton shape="block" width="full" size="xl" />
                  </CardContent>
                </Card>
              ))}
            </Stack>
          </SidebarLayoutMain>

          <SidebarLayoutSidebar>
            {/* Source card: Marketplace and Path. */}
            <Card column>
              <CardHeader>
                <Skeleton shape="line" width="third" size="sm" />
              </CardHeader>
              <CardContent>
                <Stack gap="cozy">
                  {Array.from({ length: 2 }).map((_, i) => (
                    <Stack key={i} gap="tight">
                      <Skeleton shape="line" width="quarter" size="xs" />
                      <Skeleton shape="line" width="three-quarters" size="sm" />
                    </Stack>
                  ))}
                </Stack>
              </CardContent>
            </Card>
          </SidebarLayoutSidebar>
        </SidebarLayout>
      </Stack>
    </Stack>
  );
}
