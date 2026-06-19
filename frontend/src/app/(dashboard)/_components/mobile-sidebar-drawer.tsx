'use client';

/** @tier 3 — dashboard-scoped. Left-side slide-in drawer that renders the full sidebar nav on mobile viewports. */

import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Badge, Button, Icon, Separator, Stack, Text } from '@orbit/terra';
import { DashboardSidebar } from './dashboard-sidebar';
import { SidebarUserFooter } from './sidebar-user-footer';
import { useSidebarState } from './sidebar-state';

// ── MobileSidebarDrawer ───────────────────────────────────────────────────────

/**
 * Full-height left-side drawer that renders the standard sidebar content.
 * Only visible on viewports narrower than `md` (768 px) — toggled by the
 * hamburger button in `DashboardHeader`.
 *
 * Built on Radix Dialog to get correct focus trapping, Escape-to-close,
 * scroll lock, and `aria-modal` for free. Passes `close` to `DashboardSidebar`
 * so nav link clicks automatically dismiss the drawer.
 */
export function MobileSidebarDrawer() {
  const { isOpen, close } = useSidebarState();

  return (
    <DialogPrimitive.Root open={isOpen} onOpenChange={(open) => !open && close()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/50 md:hidden data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-200" />
        <DialogPrimitive.Content
          className="fixed inset-y-0 left-0 z-50 w-64 flex flex-col bg-surface border-r border-border-subtle shadow-2xl md:hidden data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left-full data-[state=open]:slide-in-from-left-full duration-300"
        >
          {/* Visually hidden title for screen readers */}
          <DialogPrimitive.Title className="sr-only">Navigation</DialogPrimitive.Title>

          {/* Brand header — matches the desktop h-14 sidebar header slot */}
          <Stack
            direction="horizontal"
            align="center"
            justify="between"
            height="header"
            paddingInline="comfortable"
            shrink
          >
            <Stack direction="horizontal" align="center" gap="snug">
              <Icon name="zap" size="sm" container containerVariant="accent" />
              <Text size="supporting" weight="strong">
                Orbit
              </Text>
            </Stack>
            <Stack direction="horizontal" align="center" gap="snug">
              <Badge variant="outline" size="sm">v0.4.0</Badge>
              <DialogPrimitive.Close asChild>
                <Button variant="ghost" size="icon" aria-label="Close navigation">
                  <Icon name="close" />
                </Button>
              </DialogPrimitive.Close>
            </Stack>
          </Stack>

          <Separator />

          {/* Nav links — onNavigate closes the drawer */}
          <Stack grow overflowY="auto">
            <DashboardSidebar onNavigate={close} />
          </Stack>

          {/* User footer */}
          <SidebarUserFooter />
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
