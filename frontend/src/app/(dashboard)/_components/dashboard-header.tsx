import { Button, Icon, Separator, Stack, Text } from '@orbit/terra';

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
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="relative">
            <Button variant="ghost" size="icon" aria-label="Notifications">
              <Icon name="bell" />
            </Button>
            {/* Notification dot — decorative, aria-hidden */}
            {/* eslint-disable-next-line local/no-raw-div */}
            <div
              aria-hidden
              className="pointer-events-none absolute top-1.5 right-1.5 size-1.5 rounded-full bg-status-error"
            />
          </div>
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
