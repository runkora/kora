// Server Component: the sub-components are imported by name because terra is a
// 'use client' entrypoint and SidebarLayout.Main does not survive that boundary.
import {
  Card,
  CardContent,
  CardHeader,
  SidebarLayout,
  SidebarLayoutMain,
  SidebarLayoutSidebar,
  Skeleton,
  Stack,
} from '@orbit/terra';

/**
 * The Skill detail page loads one Skill in one server pass, so the skeleton
 * stands in for the whole surface: the back link, the identity header, the
 * files, and the Source sidebar.
 */
export default function SkillDetailLoading() {
  return (
    <Stack gap="comfortable">
      {/* Back link to the Skills overview. */}
      <Stack direction="horizontal">
        <Skeleton shape="block" width="quarter" size="md" />
      </Stack>

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
