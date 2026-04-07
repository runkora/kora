import { Bell, Bot, ChevronRight, RefreshCw } from 'lucide-react';

import { Button, Separator, Stack } from '@orbit/terra';

/**
 * Top header bar for the dashboard shell.
 *
 * Contains breadcrumb navigation, quick actions (refresh, notifications),
 * and the primary "Deploy Agent" CTA.
 *
 * Rendered as a server component — no client-side state required.
 * The `PageLayout` header slot constrains this to h-14 and handles overflow.
 */
export function DashboardHeader() {
  return (
    <Stack direction="horizontal" align="center" justify="between" asChild>
      <header className="h-full px-6">
        {/* TODO: Replace with a Terra native Breadcrumbs component */}
        {/* Breadcrumb — raw spans: breadcrumb is a navigation pattern, not body prose */}
        <Stack direction="horizontal" align="center" asChild>
          <nav className="gap-1.5 text-sm text-muted">
            <span>Orbit</span>
            <ChevronRight className="size-3.5" aria-hidden />
            <span className="text-foreground font-medium">Dashboard</span>
          </nav>
        </Stack>

        {/* Actions */}
        <Stack direction="horizontal" align="center" gap="snug">
          <Button variant="ghost" size="icon" aria-label="Refresh">
            <RefreshCw className="size-4" />
          </Button>
          <span className="relative">
            <Button variant="ghost" size="icon" aria-label="Notifications">
              <Bell className="size-4" />
            </Button>
            {/* Notification dot — decorative, aria-hidden */}
            <span
              aria-hidden
              className="pointer-events-none absolute top-1.5 right-1.5 size-1.5 rounded-full bg-status-error"
            />
          </span>
          <Separator orientation="vertical" span="narrow" />
          <Button variant="default" size="sm">
            <Bot className="size-4" />
            Deploy Agent
          </Button>
        </Stack>
      </header>
    </Stack>
  );
}
