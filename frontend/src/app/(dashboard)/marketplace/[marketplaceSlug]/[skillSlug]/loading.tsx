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
 * The Skill page loads the Marketplaces and the Skill in one server pass, so
 * the skeleton stands in for the whole surface: the file explorer, the
 * preview of the selected file, and the repository card.
 */
export default function SkillDetailLoading() {
  return (
    <Stack role="status" aria-label="Loading the Skill">
      <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
        <SidebarLayoutMain>
          <Stack gap="comfortable">
            {/* File explorer: header and a few file rows. */}
            <Card column>
              <CardHeader>
                <Skeleton shape="line" width="third" size="sm" />
              </CardHeader>
              <CardContent>
                <Stack gap="snug">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} shape="line" width="half" size="sm" />
                  ))}
                </Stack>
              </CardContent>
            </Card>

            {/* Preview of the selected file. */}
            <Card column>
              <CardHeader>
                <Skeleton shape="line" width="quarter" size="sm" />
              </CardHeader>
              <CardContent>
                <Skeleton shape="block" width="full" size="xl" />
              </CardContent>
            </Card>
          </Stack>
        </SidebarLayoutMain>

        <SidebarLayoutSidebar>
          {/* Repository card: owner and owner/repo. */}
          <Card column>
            <CardContent padded>
              <Stack gap="tight">
                <Skeleton shape="line" width="half" size="sm" />
                <Skeleton shape="line" width="three-quarters" size="xs" />
              </Stack>
            </CardContent>
          </Card>
        </SidebarLayoutSidebar>
      </SidebarLayout>
    </Stack>
  );
}
