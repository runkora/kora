'use client';

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { useRouter } from 'next/navigation';
import {
  BottomSheet,
  Button,
  Icon,
  NavItem,
  SectionLabel,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import { TenantList, useTenantSwitch } from '@runkora/components';
import { useBrand } from '@runkora/theme/brand-provider';
import { APP_ROUTES } from '@runkora/lib/routing/routes';
import { startTenantSwitch } from '@runkora/lib/tenant-switch';
import { useLogout } from '@runkora/lib/use-logout';
import type { TenantOption } from '@runkora/server/auth-config';
import { useSession } from './permissions-provider';
import { FitEmailText } from './fit-email-text';

// ── Responsive shell selection ─────────────────────────────────────────────
//
// Hades components can't take a raw `className` outside the Hades package
// (`no-hades-classname` ESLint rule, no exceptions), so the usual "render
// both, toggle with `hidden md:flex`" trick this codebase uses elsewhere
// (e.g. `PageLayout`'s sidebar-vs-drawer switch, written *inside* Hades)
// isn't available here. `useSyncExternalStore` is the correct alternative —
// a `getServerSnapshot` keeps the first client render consistent with the
// server, corrected once mounted (a one-time flash, not a hydration error).

const DESKTOP_QUERY = '(min-width: 768px)';

function subscribeToDesktopQuery(onChange: () => void) {
  const query = window.matchMedia(DESKTOP_QUERY);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

function useIsDesktop(): boolean {
  return useSyncExternalStore(
    subscribeToDesktopQuery,
    () => window.matchMedia(DESKTOP_QUERY).matches,
    () => true,
  );
}

function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const query = window.matchMedia('(prefers-reduced-motion: reduce)');
      query.addEventListener('change', onChange);
      return () => query.removeEventListener('change', onChange);
    },
    () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    () => false,
  );
}

// Zero-width space after '@' so a long email prefers wrapping there — the
// spec's own reference inserts a literal <wbr>, but raw phrasing-content
// elements are banned in product code here; a zero-width space is the
// equivalent soft break opportunity without an element.
function withSoftEmailBreak(value: string): string {
  return value.replace('@', '@​');
}

// ── Shared account-menu state ───────────────────────────────────────────────

function useAccountMenu(onLogout: () => void) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);

  const close = useCallback(() => {
    setOpen(false);
    setConfirming(false);
  }, []);

  const openMenu = useCallback(() => setOpen(true), []);
  const requestLogout = useCallback(() => setConfirming(true), []);
  const cancelLogout = useCallback(() => setConfirming(false), []);
  const confirmLogout = useCallback(() => {
    close();
    onLogout();
  }, [close, onLogout]);

  return { open, setOpen, confirming, close, openMenu, requestLogout, cancelLogout, confirmLogout };
}

// ── Desktop: outside-click + Esc dismissal ──────────────────────────────────
//
// The flyout is explicitly NOT modal (spec section 07): no focus trap, no
// scrim, page stays scrollable/interactive — so dismissal is a plain
// outside-pointerdown + Escape listener, not Radix's Dialog machinery (that's
// reserved for the mobile BottomSheet, which *is* modal).

function useDismissOnOutsideOrEscape(
  active: boolean,
  containerRef: React.RefObject<HTMLElement | null>,
  onDismiss: () => void,
) {
  useEffect(() => {
    if (!active) return;
    function handlePointerDown(event: PointerEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        onDismiss();
      }
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onDismiss();
    }
    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [active, containerRef, onDismiss]);
}

// ── Menu item ───────────────────────────────────────────────────────────────

// Session actions use the sidebar's NavItem so their size and inset match the nav rows.
function MenuItem({
  icon,
  danger,
  onSelect,
  itemRef,
  role = 'menuitem',
  children,
}: {
  icon: React.ComponentProps<typeof Icon>['name'];
  danger?: boolean;
  onSelect: () => void;
  itemRef?: React.Ref<HTMLElement>;
  /** `'menuitem'` inside the desktop `role="menu"`; unset in the mobile dialog. */
  role?: 'menuitem';
  children: React.ReactNode;
}) {
  return (
    <NavItem
      ref={itemRef}
      role={role}
      onClick={onSelect}
      // A NavItem button inherits the button's centred text; the sidebar's link rows start at the left.
      style={{ textAlign: 'start' }}
      icon={<Icon name={icon} color={danger ? 'status-error' : 'muted'} />}
    >
      <Text
        as="span"
        color={danger ? 'status-error' : undefined}
        style={danger ? { font: 'inherit' } : { font: 'inherit', color: 'inherit' }}
      >
        {children}
      </Text>
    </NavItem>
  );
}

// ── Shared identity block ("Signed in as" / name / email) ──────────────────

function IdentityBlock({
  name,
  email,
  compact,
}: {
  name: string;
  email: string;
  compact: boolean;
}) {
  return (
    <Stack gap="cozy">
      <SectionLabel>Signed in as</SectionLabel>
      <Stack gap="tight">
        {name && (
          <Text
            as="span"
            style={{
              display: 'block',
              fontSize: compact ? 14 : 16,
              lineHeight: compact ? '20px' : '24px',
              fontWeight: 500,
            }}
          >
            {name}
          </Text>
        )}
        <Text
          as="span"
          color="subtle"
          style={{
            display: 'block',
            fontSize: compact ? 12 : 13,
            lineHeight: compact ? '16px' : '18px',
            overflowWrap: 'anywhere',
          }}
        >
          {withSoftEmailBreak(email)}
        </Text>
      </Stack>
    </Stack>
  );
}

// ── Tenant section (two or more tenants only) ──────────────────────────────

function TenantSection({
  tenants,
  tenantSwitch,
  onPickActive,
}: {
  tenants: TenantOption[];
  tenantSwitch: ReturnType<typeof useTenantSwitch>;
  onPickActive: () => void;
}) {
  return (
    <Stack gap="snug">
      <SectionLabel>Tenant</SectionLabel>
      {/* Rows bleed out by their own padding, as in the sidebar nav, so row text meets the labels' edge. */}
      <Stack style={{ marginInline: -12 }}>
        <TenantList
          tenants={tenants}
          switching={tenantSwitch.switching}
          failed={tenantSwitch.failed}
          onPick={(t) => (t.active ? onPickActive() : tenantSwitch.start(t.id))}
        />
      </Stack>
    </Stack>
  );
}

// ── Shared confirm-logout block ──────────────────────────────────────────────

function ConfirmLogoutBlock({ compact }: { compact: boolean }) {
  const { productName } = useBrand();
  return (
    <>
      <Text as="span" style={{ display: 'block', fontSize: compact ? 14 : 16, fontWeight: 500 }}>
        Log out of {productName}?
      </Text>
      <Text
        as="span"
        color="muted"
        style={{
          display: 'block',
          marginTop: 4,
          fontSize: compact ? 12 : 13,
          lineHeight: 1.4,
        }}
      >
        You&rsquo;ll need to sign in again to continue.
      </Text>
    </>
  );
}

/**
 * User identity + account menu rendered inside PageLayout's sidebarFooter
 * slot. PageLayout wraps this in a semantic <footer> element internally.
 *
 * Desktop (≥768px): the trigger opens a flyout to the right of the sidebar
 * (identity panel above a divider, actions panel below it). It is not modal,
 * has no scrim, and is dismissed by outside-click or Escape.
 *
 * Mobile (<768px): the same trigger opens a `BottomSheet` (Hades) instead —
 * fully modal, with its own drag-to-dismiss, close button, and back-button
 * integration.
 *
 * Log out requires an in-place confirm step (swaps panel contents, no
 * separate dialog) in both contexts. Replaces the inline LogoutButton
 * (ADR-0061).
 */
export function SidebarUserFooter() {
  const { user, tenants = [] } = useSession();
  const { logout } = useLogout();
  const router = useRouter();
  const isDesktop = useIsDesktop();
  const reducedMotion = usePrefersReducedMotion();
  const flyoutAnimation = reducedMotion
    ? undefined
    : 'account-flyout-in 140ms cubic-bezier(0,0,0.2,1) both';

  const email = user?.email || 'Loading…';
  // An empty display name is a legitimate, reachable session value (the auth
  // route falls back to `''` when the JWT has no `display_name` claim) — it
  // is intentionally NOT defaulted to `email` here, which would otherwise
  // render the email twice (once as "name", once as itself).
  const name = user?.name ?? '';
  const hasName = Boolean(name);

  const handleLogout = useCallback(() => {
    void logout();
  }, [logout]);

  const menu = useAccountMenu(handleLogout);
  const { open, confirming } = menu;

  // With two or more tenants the trigger names the active one and the menu lists them all.
  const multiTenant = tenants.length > 1;
  const activeTenant = tenants.find((t) => t.active);
  // A switch returns to the page it started on, now in the other tenant.
  const tenantSwitch = useTenantSwitch((tenantId) =>
    startTenantSwitch({
      email: user?.email ?? '',
      tenantId,
      returnPath: window.location.pathname + window.location.search,
    }),
  );

  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const firstItemRef = useRef<HTMLButtonElement>(null);
  const secondItemRef = useRef<HTMLButtonElement>(null);
  const cancelButtonRef = useRef<HTMLButtonElement>(null);

  // The confirm step replaces the menu items with plain Cancel/Log out
  // buttons (see the `role="menu"` note below) — without this, focus is
  // silently dropped when the "Log out" item that had it unmounts.
  useEffect(() => {
    if (confirming) cancelButtonRef.current?.focus();
  }, [confirming]);

  const closeAndFocusTrigger = useCallback(() => {
    menu.close();
    triggerRef.current?.focus();
  }, [menu]);

  useDismissOnOutsideOrEscape(isDesktop && open, containerRef, closeAndFocusTrigger);

  const handleViewProfile = useCallback(() => {
    menu.close();
    router.push(APP_ROUTES.profile);
  }, [menu, router]);

  const handleTriggerKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown' && !open) {
      event.preventDefault();
      menu.openMenu();
      requestAnimationFrame(() => firstItemRef.current?.focus());
    }
  };

  const handleTriggerClick = () => {
    if (open) {
      menu.close();
    } else {
      menu.openMenu();
      if (document.activeElement === triggerRef.current) {
        requestAnimationFrame(() => firstItemRef.current?.focus());
      }
    }
  };

  const handleMenuKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const items = [firstItemRef.current, secondItemRef.current].filter(
      (el): el is HTMLButtonElement => el !== null,
    );
    const currentIndex = items.findIndex((el) => el === document.activeElement);

    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      let nextIndex = currentIndex;
      if (event.key === 'ArrowDown') nextIndex = (currentIndex + 1) % items.length;
      else if (event.key === 'ArrowUp')
        nextIndex = (currentIndex - 1 + items.length) % items.length;
      else if (event.key === 'Home') nextIndex = 0;
      else if (event.key === 'End') nextIndex = items.length - 1;
      items[nextIndex]?.focus();
    }
  };

  const avatar = (
    <Stack
      align="center"
      justify="center"
      style={{
        width: 32,
        height: 32,
        borderRadius: 9999,
        backgroundColor: 'var(--color-surface-overlay)',
        flexShrink: 0,
      }}
    >
      <Icon name="user" size="sm" color="muted" />
    </Stack>
  );

  const trigger = (
    <Button
      ref={triggerRef}
      variant="ghost"
      fullWidth
      aria-haspopup={isDesktop ? 'menu' : 'dialog'}
      aria-expanded={open}
      aria-controls={isDesktop ? 'account-menu' : undefined}
      data-state={open ? 'open' : 'closed'}
      onClick={handleTriggerClick}
      onKeyDown={handleTriggerKeyDown}
      style={{
        height: 48,
        padding: '0 8px',
        gap: 10,
        justifyContent: 'flex-start',
        textAlign: 'left',
        backgroundColor: open ? 'var(--color-surface-overlay)' : undefined,
      }}
    >
      {avatar}
      <Stack
        direction="vertical"
        grow
        minWidth="none"
        gap="none"
        justify={hasName || multiTenant ? undefined : 'center'}
        style={{ overflow: 'hidden' }}
      >
        {multiTenant ? (
          <>
            <Text as="span" size="caption" weight="strong" truncate>
              {email}
            </Text>
            <Text as="span" size="micro" color="subtle" truncate>
              {activeTenant?.displayName}
            </Text>
          </>
        ) : (
          <>
            {hasName && (
              <Text
                as="span"
                truncate
                style={{ display: 'block', fontSize: 13, lineHeight: '16px', fontWeight: 600 }}
              >
                {name}
              </Text>
            )}
            <Stack direction="horizontal" align="center" style={{ height: 16, overflow: 'hidden' }}>
              <FitEmailText email={email} />
            </Stack>
          </>
        )}
      </Stack>
      <Icon name={isDesktop ? 'chevron-right' : 'chevron-up'} size="xs" color="subtle" shrink />
    </Button>
  );

  return (
    <>
      <Separator />
      <Stack ref={containerRef} position="relative" padding="snug">
        {trigger}

        {isDesktop && open && (
          // One column anchored to the footer, so the action rows can take their own height.
          <Stack
            style={{
              position: 'absolute',
              bottom: 0,
              left: 'calc(100% + 1px)',
              zIndex: 50,
              width: 280,
              borderTop: '1px solid var(--color-border-subtle)',
              borderRight: '1px solid var(--color-border-subtle)',
              borderTopRightRadius: 8,
              backgroundColor: 'var(--color-background)',
              boxShadow: '12px 0 24px -12px var(--color-overlay-strong)',
              animation: flyoutAnimation,
            }}
          >
            <Stack id="account-identity" padding="comfortable">
              {confirming ? (
                <ConfirmLogoutBlock compact />
              ) : (
                <Stack gap="comfortable">
                  <IdentityBlock name={name} email={email} compact />
                  {multiTenant ? (
                    <>
                      <Separator />
                      <TenantSection
                        tenants={tenants}
                        tenantSwitch={tenantSwitch}
                        onPickActive={closeAndFocusTrigger}
                      />
                    </>
                  ) : null}
                </Stack>
              )}
            </Stack>
            <Separator />
            <Stack
              id="account-menu"
              role={confirming ? undefined : 'menu'}
              aria-label={confirming ? undefined : 'Account'}
              aria-describedby="account-identity"
              onKeyDown={confirming ? undefined : handleMenuKeyDown}
              gap="tight"
              padding={confirming ? 'comfortable' : 'tight'}
            >
              {confirming ? (
                <Stack direction="horizontal" gap="snug">
                  <Button
                    ref={cancelButtonRef}
                    variant="outline"
                    size="sm"
                    style={{ flex: 1 }}
                    onClick={menu.cancelLogout}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="destructive"
                    size="sm"
                    style={{ flex: 1 }}
                    onClick={menu.confirmLogout}
                  >
                    Log out
                  </Button>
                </Stack>
              ) : (
                <>
                  <MenuItem icon="user" onSelect={handleViewProfile} itemRef={firstItemRef}>
                    View profile
                  </MenuItem>
                  <MenuItem
                    icon="log-out"
                    danger
                    onSelect={menu.requestLogout}
                    itemRef={secondItemRef}
                  >
                    Log out
                  </MenuItem>
                </>
              )}
            </Stack>
          </Stack>
        )}
      </Stack>

      {!isDesktop && (
        <BottomSheet
          open={open}
          onOpenChange={(next) => (next ? menu.openMenu() : menu.close())}
          title="Account"
        >
          <Stack style={{ padding: '12px 20px 16px' }}>
            {confirming ? (
              <ConfirmLogoutBlock compact={false} />
            ) : (
              <Stack gap="comfortable">
                <IdentityBlock name={name} email={email} compact={false} />
                {multiTenant ? (
                  <>
                    <Separator />
                    <TenantSection
                      tenants={tenants}
                      tenantSwitch={tenantSwitch}
                      onPickActive={menu.close}
                    />
                  </>
                ) : null}
              </Stack>
            )}
          </Stack>
          <Separator />
          {confirming ? (
            <Stack direction="horizontal" style={{ gap: 8, padding: '12px 20px 4px' }}>
              <Button
                ref={cancelButtonRef}
                variant="outline"
                style={{ flex: 1, height: 44 }}
                onClick={menu.cancelLogout}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                style={{ flex: 1, height: 44 }}
                onClick={menu.confirmLogout}
              >
                Log out
              </Button>
            </Stack>
          ) : (
            <Stack padding="snug">
              <MenuItem icon="user" onSelect={handleViewProfile} role={undefined}>
                View profile
              </MenuItem>
              <MenuItem icon="log-out" danger onSelect={menu.requestLogout} role={undefined}>
                Log out
              </MenuItem>
            </Stack>
          )}
        </BottomSheet>
      )}
    </>
  );
}
