'use client';

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { useRouter } from 'next/navigation';
import { BottomSheet, Button, Icon, SectionLabel, Separator, Stack, Text } from '@orbit/hades';
import { APP_ROUTES } from '@orbit/lib/routing/routes';
import { useLogout } from '@orbit/lib/use-logout';
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

// ── Menu item (desktop + mobile share the same semantics, different sizing) ─

interface MenuItemProps {
  icon: React.ReactNode;
  tone?: 'default' | 'danger';
  height: number;
  fontSize: number;
  onSelect: () => void;
  itemRef?: React.Ref<HTMLButtonElement>;
  /** `'menuitem'` inside a `role="menu"` ancestor (desktop); `undefined` when
   *  reused as a plain action row with no owning menu (the mobile dialog). */
  role?: 'menuitem';
  children: React.ReactNode;
}

function MenuItem({
  icon,
  tone = 'default',
  height,
  fontSize,
  onSelect,
  itemRef,
  role = 'menuitem',
  children,
}: MenuItemProps) {
  return (
    <Button
      ref={itemRef}
      variant="ghost"
      fullWidth
      role={role}
      onClick={onSelect}
      style={{
        height,
        padding: '0 12px',
        gap: 10,
        justifyContent: 'flex-start',
        fontSize,
        fontWeight: 500,
        lineHeight: 1,
      }}
    >
      {icon}
      <Text
        as="span"
        color={tone === 'danger' ? 'status-error' : 'muted'}
        style={{ fontSize: 'inherit', fontWeight: 'inherit', lineHeight: 'inherit' }}
      >
        {children}
      </Text>
    </Button>
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
    <>
      <Stack asChild marginBottom="tight">
        <SectionLabel>Signed in as</SectionLabel>
      </Stack>
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
          marginTop: name ? (compact ? 2 : 8) : 0,
          fontSize: compact ? 12 : 13,
          lineHeight: compact ? '16px' : '18px',
          overflowWrap: 'anywhere',
        }}
      >
        {withSoftEmailBreak(email)}
      </Text>
    </>
  );
}

// ── Shared confirm-logout block ──────────────────────────────────────────────

function ConfirmLogoutBlock({ compact }: { compact: boolean }) {
  return (
    <>
      <Text as="span" style={{ display: 'block', fontSize: compact ? 14 : 16, fontWeight: 500 }}>
        Log out of kora?
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
 * Desktop (≥768px): the trigger opens a two-panel flyout to the right of the
 * sidebar (identity panel above a shared divider, actions panel below it,
 * sized to the footer's own height) — not modal, no scrim, dismissed by
 * outside-click or Escape.
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
  const { user } = useSession();
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
        justify={hasName ? undefined : 'center'}
        style={{ overflow: 'hidden' }}
      >
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
          <>
            <Stack
              id="account-identity"
              style={{
                position: 'absolute',
                bottom: 'calc(100% + 1px)',
                left: 'calc(100% + 1px)',
                zIndex: 50,
                width: 240,
                padding: '12px 20px',
                borderTop: '1px solid var(--color-border-subtle)',
                borderRight: '1px solid var(--color-border-subtle)',
                borderTopRightRadius: 8,
                backgroundColor: 'var(--color-background)',
                boxShadow: '12px 0 24px -12px var(--color-overlay-strong)',
                animation: flyoutAnimation,
              }}
            >
              {confirming ? (
                <ConfirmLogoutBlock compact />
              ) : (
                <IdentityBlock name={name} email={email} compact />
              )}
            </Stack>

            <Stack
              id="account-menu"
              role={confirming ? undefined : 'menu'}
              aria-label={confirming ? undefined : 'Account'}
              aria-describedby="account-identity"
              onKeyDown={confirming ? undefined : handleMenuKeyDown}
              gap="none"
              style={{
                position: 'absolute',
                top: -1,
                bottom: 0,
                left: 'calc(100% + 1px)',
                zIndex: 50,
                width: 240,
                padding: '4px 8px',
                justifyContent: 'center',
                borderTop: '1px solid var(--color-border-subtle)',
                borderRight: '1px solid var(--color-border-subtle)',
                backgroundColor: 'var(--color-background)',
                boxShadow: '12px 0 24px -12px var(--color-overlay-strong)',
                animation: flyoutAnimation,
              }}
            >
              {confirming ? (
                <Stack direction="horizontal" style={{ gap: 6, padding: '0 4px' }}>
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
                  <MenuItem
                    icon={<Icon name="user" size="sm" color="muted" />}
                    height={28}
                    fontSize={14}
                    onSelect={handleViewProfile}
                    itemRef={firstItemRef}
                  >
                    View profile
                  </MenuItem>
                  <MenuItem
                    icon={<Icon name="log-out" size="sm" color="status-error" />}
                    tone="danger"
                    height={28}
                    fontSize={14}
                    onSelect={menu.requestLogout}
                    itemRef={secondItemRef}
                  >
                    Log out
                  </MenuItem>
                </>
              )}
            </Stack>
          </>
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
              <IdentityBlock name={name} email={email} compact={false} />
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
            <Stack style={{ padding: 8, gap: 12 }}>
              <MenuItem
                icon={<Icon name="user" size="md" color="muted" />}
                height={48}
                fontSize={15}
                onSelect={handleViewProfile}
                role={undefined}
              >
                View profile
              </MenuItem>
              <MenuItem
                icon={<Icon name="log-out" size="md" color="status-error" />}
                tone="danger"
                height={48}
                fontSize={15}
                onSelect={menu.requestLogout}
                role={undefined}
              >
                Log out
              </MenuItem>
            </Stack>
          )}
        </BottomSheet>
      )}
    </>
  );
}
