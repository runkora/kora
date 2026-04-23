import { Button, Icon, Separator, Stack, StatusDot, Text } from '@orbit/terra';

/**
 * Top header bar for the dashboard shell.
 *
 * Contains breadcrumb navigation, quick actions (refresh, notifications),
 * and the primary "Deploy Agent" CTA.
 *
 * Rendered as a server component — no client-side state required.
 */
export function DashboardHeader() {
  return (
    <Stack direction="horizontal" align="center" justify="between" asChild>
      <header className="h-full px-6">
        {/* TODO: Replace with Terra Breadcrumbs component when available */}
        <Stack direction="horizontal" align="center" asChild>
          <nav className="gap-1.5">
            <Text size="supporting" color="muted">
              Orbit
            </Text>
            <Icon name="chevron-right" size="2xs" label="" />
            <Text size="supporting" weight="emphasis">
              Dashboard
            </Text>
          </nav>
        </Stack>

        {/* Actions */}
        <Stack direction="horizontal" align="center" gap="snug">
          <Button variant="ghost" size="icon" aria-label="Refresh">
            <Icon name="refresh-cw" />
          </Button>
          {/* Positioning context for the notification dot */}
          <Stack style={{ position: 'relative' }}>
            <Button variant="ghost" size="icon" aria-label="Notifications">
              <Icon name="bell" />
            </Button>
            {/* Notification dot — decorative, aria-hidden */}
            <StatusDot
              aria-hidden
              status="error"
              size="sm"
              style={{ position: 'absolute', top: '0.375rem', right: '0.375rem', pointerEvents: 'none' }}
            />
          </Stack>
          <Separator orientation="vertical" span="narrow" />
          <Button variant="default" size="sm">
            <Icon name="bot" />
            Deploy Agent
          </Button>
        </Stack>
      </header>
    </Stack>
  );
}
