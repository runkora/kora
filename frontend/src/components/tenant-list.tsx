/** @tier 2 — kora-domain-aware: lists the person's tenants. Used by: dashboard account menu, MCP consent, sign-in tenant picker (search). */
'use client';

import { useEffect, useRef, useState } from 'react';
import { Icon, Input, NavItem, Stack, Text } from '@runkora/hades';
import type { TenantOption } from '@runkora/server/auth-config';

/** Above this many tenants a tenant list gains a search field. */
const SEARCH_THRESHOLD = 6;

/** Shown when the search matches no tenant. */
export const NO_TENANTS_MATCH = 'No tenants match.';

/** The search over a tenant list: the query, whether the field shows, and the rows that match. */
export function useTenantSearch<T extends { displayName: string }>(tenants: T[]) {
  const [query, setQuery] = useState('');
  const q = query.trim().toLowerCase();
  const shown = q ? tenants.filter((t) => t.displayName.toLowerCase().includes(q)) : tenants;
  return { query, setQuery, searchable: tenants.length > SEARCH_THRESHOLD, shown };
}

/** Fades the bottom edge of the scrolling list while more rows sit below it. */
const FADE = 'linear-gradient(to bottom, black calc(100% - 2rem), transparent)';

/**
 * Tracks a tenant switch: the tenant it is starting and whether the last one failed to start.
 * `onSelect` starts the switch; a rejection or a throw marks it failed. `clearError` hides the
 * error line, for a menu that opens again.
 */
export function useTenantSwitch(onSelect: (tenantId: string) => Promise<void> | void) {
  const [switching, setSwitching] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  const start = async (tenantId: string) => {
    setSwitching(tenantId);
    setFailed(false);
    try {
      await onSelect(tenantId);
    } catch {
      setSwitching(null);
      setFailed(true);
    }
  };
  return { switching, failed, start, clearError: () => setFailed(false) };
}

export interface TenantListProps {
  tenants: TenantOption[];
  /** The tenant a switch is starting for. Disables the list while set. */
  switching: string | null;
  /** Shows the error line under the rows. */
  failed: boolean;
  onPick: (tenant: TenantOption) => void;
}

/**
 * Rows styled as sidebar nav items (text inset by the row's own padding), a check on the active tenant, a search field above six tenants, the error
 * line when a switch fails and a note that switching signs the person in again.
 */
export function TenantList({ tenants, switching, failed, onPick }: TenantListProps) {
  const { query, setQuery, searchable, shown } = useTenantSearch(tenants);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [moreBelow, setMoreBelow] = useState(false);

  // Shows the fade only while the list overflows and is not scrolled to the end.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !searchable) return;
    const update = () => setMoreBelow(el.scrollHeight - el.scrollTop - el.clientHeight > 1);
    update();
    el.addEventListener('scroll', update);
    return () => el.removeEventListener('scroll', update);
  }, [searchable, shown.length]);

  return (
    <Stack gap="tight">
      {searchable ? (
        <Input
          value={query}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setQuery(e.target.value)}
          placeholder="Search tenants"
          aria-label="Search tenants"
        />
      ) : null}
      <Stack
        ref={scrollRef}
        {...(searchable ? ({ maxHeight: 'scroll-md', overflowY: 'auto' } as const) : {})}
        style={moreBelow ? { maskImage: FADE, WebkitMaskImage: FADE } : undefined}
      >
        <Stack gap="none" shrink aria-label="Tenants">
          {shown.map((t) => (
            <NavItem
              key={t.id}
              state={switching !== null ? 'disabled' : t.active ? 'active' : 'default'}
              aria-current={t.active ? 'true' : undefined}
              // NavItem has no disabled prop; its disabled state blocks the pointer and this guards the keyboard.
              aria-disabled={switching !== null}
              title={t.displayName}
              onClick={() => (switching === null ? onPick(t) : undefined)}
            >
              <Stack
                direction="horizontal"
                align="center"
                justify="between"
                gap="snug"
                minWidth="none"
              >
                <Text as="span" truncate style={{ font: 'inherit', color: 'inherit' }}>
                  {t.displayName}
                </Text>
                {t.active ? <Icon name="check" size="xs" color="accent" /> : null}
                {switching === t.id ? <Icon name="loader" size="xs" color="subtle" /> : null}
              </Stack>
            </NavItem>
          ))}
          {shown.length === 0 ? (
            <Stack paddingInline="cozy" paddingBlock="snug">
              <Text size="caption" color="subtle">
                {NO_TENANTS_MATCH}
              </Text>
            </Stack>
          ) : null}
        </Stack>
      </Stack>
      {failed ? (
        <Stack paddingInline="cozy" paddingBlock="snug">
          <Text size="caption" color="status-error" role="alert">
            Could not start the switch. Try again.
          </Text>
        </Stack>
      ) : null}
      <Stack paddingInline="cozy" paddingBottom="tight">
        <Text size="micro" color="subtle">
          {switching ? 'Signing you in to that tenant.' : 'Switching signs you in again.'}
        </Text>
      </Stack>
    </Stack>
  );
}
