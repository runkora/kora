import { Badge, Icon, Stack, Text } from '@orbit/terra';

export function SidebarBrandHeader() {
  return (
    // height="full" fills the h-14 sidebarHeader slot in PageLayout, which matches
    // the page header height so both bottom borders align on the same pixel row.
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      height="full"
      paddingInline="comfortable"
    >
      <Stack direction="horizontal" align="center" gap="snug">
        <Icon name="zap" size="sm" container containerVariant="accent" />
        <Text size="supporting" weight="strong">
          Orbit
        </Text>
      </Stack>
      <Badge variant="outline" size="sm">
        v0.4.0
      </Badge>
    </Stack>
  );
}
