// Server Component: the sub-components are imported by name because hades is a
// 'use client' entrypoint and SidebarLayout.Main does not survive that boundary.
import {
  Card,
  CardContent,
  SidebarLayout,
  SidebarLayoutMain,
  SidebarLayoutSidebar,
  Skeleton,
  Stack,
} from '@clear-route/hades';

/**
 * The MCP server detail page fetches one server's metadata and tool catalog in
 * one server pass, so the skeleton stands in for the whole surface: the
 * identity header, the tool catalog list, and the connect/trust sidebar.
 */
export default function McpServerDetailLoading() {
  return (
    <Stack gap="relaxed">
      {/* Identity header: avatar + name, health pill, and description. */}
      <Stack direction="horizontal" align="start" gap="comfortable">
        <Skeleton shape="circle" size="xl" />
        <Stack gap="tight" grow>
          <Skeleton shape="line" width="third" size="lg" />
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
      </Stack>

      {/* Body: tool catalog left, connect + trust sidebar right. */}
      <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
        <SidebarLayoutMain>
          <Stack gap="comfortable">
            <Skeleton shape="line" width="quarter" size="sm" />
            <Card column>
              <CardContent padded>
                <Stack gap="comfortable">
                  {Array.from({ length: 6 }).map((_, i) => (
                    <Stack key={i} direction="horizontal" align="center" gap="snug">
                      <Skeleton shape="circle" size="md" />
                      <Stack gap="tight" grow>
                        <Skeleton shape="line" width="third" size="sm" />
                        <Skeleton shape="line" width="three-quarters" size="xs" />
                      </Stack>
                    </Stack>
                  ))}
                </Stack>
              </CardContent>
            </Card>
          </Stack>
        </SidebarLayoutMain>

        <SidebarLayoutSidebar>
          <Stack gap="comfortable">
            <Card column inset border="subtle">
              <CardContent padded>
                <Stack gap="snug">
                  <Skeleton shape="line" width="half" size="sm" />
                  <Skeleton shape="block" width="full" size="xl" />
                </Stack>
              </CardContent>
            </Card>
            <Card column inset border="subtle">
              <CardContent padded>
                <Stack gap="snug">
                  <Skeleton shape="line" width="half" size="sm" />
                  <Skeleton shape="line" width="full" size="xs" />
                  <Skeleton shape="line" width="three-quarters" size="xs" />
                  <Skeleton shape="line" width="full" size="xs" />
                </Stack>
              </CardContent>
            </Card>
          </Stack>
        </SidebarLayoutSidebar>
      </SidebarLayout>
    </Stack>
  );
}
