import { Bell, Bot, ChevronRight, RefreshCw } from 'lucide-react';

import { Button, Separator } from '@orbit/terra';

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
    <div className="flex h-full items-center justify-between px-6">
      {/* TODO: Replace with a Terra native Breadcrumbs component */}
      {/* Breadcrumb — raw spans: breadcrumb is a navigation pattern, not body prose */}
      <div className="flex items-center gap-1.5 text-sm text-muted">
        <span>Orbit</span>
        <ChevronRight className="size-3.5" aria-hidden />
        <span className="text-foreground font-medium">Dashboard</span>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="icon" aria-label="Refresh">
          <RefreshCw className="size-4" />
        </Button>
        <div className="relative">
          <Button variant="ghost" size="icon" aria-label="Notifications">
            <Bell className="size-4" />
          </Button>
          {/* Notification dot — decorative, aria-hidden */}
          <span
            aria-hidden
            className="pointer-events-none absolute top-1.5 right-1.5 size-1.5 rounded-full bg-status-error"
          />
        </div>
        <Separator orientation="vertical" span="narrow" />
        <Button variant="default" size="sm">
          <Bot className="size-4" />
          Deploy Agent
        </Button>
      </div>
    </div>
  );
}
