'use client';

/** @tier 3 — dashboard-scoped. Left-side slide-in drawer that renders the full sidebar nav on mobile viewports. */

import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Button, Icon, Separator, Stack } from '@orbit/terra';
import { DashboardSidebar } from './dashboard-sidebar';
import { SidebarBrandHeader } from './sidebar-brand-header';
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
        <DialogPrimitive.Overlay className="fixed inset-0 z-[39] bg-overlay-strong md:hidden data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-200" />
        <DialogPrimitive.Content className="fixed inset-y-0 left-0 z-40 w-[300px] max-w-[85vw] flex flex-col bg-surface border-r border-border-subtle shadow-2xl pt-[env(safe-area-inset-top)] md:hidden data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left-full data-[state=open]:slide-in-from-left-full duration-300">
          {/* Visually hidden title for screen readers */}
          <DialogPrimitive.Title className="sr-only">Navigation</DialogPrimitive.Title>

          {/* Brand header — reuses the desktop SidebarBrandHeader (single source
              of the brand markup + version string) and grafts on a close button.
              `height="header"` fills the same h-14 slot as the desktop header. */}
          <Stack height="header" shrink>
            <SidebarBrandHeader
              action={
                <DialogPrimitive.Close asChild>
                  <Button variant="ghost" size="icon" aria-label="Close navigation">
                    <Icon name="close" />
                  </Button>
                </DialogPrimitive.Close>
              }
            />
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
