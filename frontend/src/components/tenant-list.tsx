/** @tier 2 — kora-domain-aware: lists the person's tenants. Used by: dashboard account menu, MCP consent. */
'use client';

import { useEffect, useState } from 'react';
import { Button, Icon, Input, Stack, Text } from '@runkora/hades';
import type { TenantOption } from '@runkora/server/auth-config';

/** Above this many tenants the list gains a search field and scrolls. */
const SEARCH_THRESHOLD = 6;

/**
 * Tracks a tenant switch: the tenant it is starting and whether the last one failed to start.
 * `onSelect` starts the switch; a rejection marks it failed.
 */
export function useTenantSwitch(onSelect: (tenantId: string) => Promise<void> | void) {
  const [switching, setSwitching] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  // A page restored from the back-forward cache after the redirect keeps its old state.
  useEffect(() => {
    const reset = (event: PageTransitionEvent) => {
      if (event.persisted) setSwitching(null);
    };
    window.addEventListener('pageshow', reset);
    return () => window.removeEventListener('pageshow', reset);
  }, []);

  const start = (tenantId: string) => {
    setSwitching(tenantId);
    setFailed(false);
    Promise.resolve(onSelect(tenantId)).catch(() => {
      setSwitching(null);
      setFailed(true);
    });
  };
  return { switching, failed, start };
}

export interface TenantListProps {
  tenants: TenantOption[];
  /** The tenant a switch is starting for. Disables the list while set. */
  switching: string | null;
  /** Shows the error line under the rows. */
  failed: boolean;
  onPick: (tenant: TenantOption) => void;
  /** `touch` gives 48px rows for the mobile sheet. */
  size?: 'compact' | 'touch';
}

/**
 * Left-aligned rows with a check on the active tenant, a search field above six tenants, the error
 * line when a switch fails and a note that switching signs the person in again.
 */
export function TenantList({
  tenants,
  switching,
  failed,
  onPick,
  size = 'compact',
}: TenantListProps) {
  const [query, setQuery] = useState('');
  const searchable = tenants.length > SEARCH_THRESHOLD;
  const q = query.trim().toLowerCase();
  const shown = q ? tenants.filter((t) => t.displayName.toLowerCase().includes(q)) : tenants;
  // Lines up the empty, error and note lines with the row text.
  const inset = size === 'touch' ? 'relaxed' : 'cozy';

  return (
    <Stack gap="tight">
      {searchable ? (
        <Input
          compact={size === 'compact'}
          value={query}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setQuery(e.target.value)}
          placeholder="Search tenants"
          aria-label="Search tenants"
        />
      ) : null}
      <Stack {...(searchable ? ({ maxHeight: 'scroll-md', overflowY: 'auto' } as const) : {})}>
        <Stack gap="none" shrink aria-label="Tenants">
          {shown.map((t) => (
            <Button
              key={t.id}
              variant="ghost"
              size={size === 'touch' ? 'xl' : 'sm'}
              fullWidth
              selected={t.active}
              aria-current={t.active ? 'true' : undefined}
              disabled={switching !== null}
              title={t.displayName}
              onClick={() => onPick(t)}
            >
              <Stack
                direction="horizontal"
                align="center"
                justify="between"
                gap="snug"
                grow
                minWidth="none"
              >
                <Text as="span" size="caption" truncate>
                  {t.displayName}
                </Text>
                {t.active ? <Icon name="check" size="xs" color="accent" /> : null}
                {switching === t.id ? <Icon name="loader" size="xs" color="subtle" /> : null}
              </Stack>
            </Button>
          ))}
          {shown.length === 0 ? (
            <Stack paddingInline={inset} paddingBlock="snug">
              <Text size="caption" color="subtle">
                No tenants match.
              </Text>
            </Stack>
          ) : null}
        </Stack>
      </Stack>
      {failed ? (
        <Stack paddingInline={inset} paddingBlock="snug">
          <Text size="caption" color="status-error" role="alert">
            Could not start the switch. Try again.
          </Text>
        </Stack>
      ) : null}
      <Stack paddingInline={inset} paddingBottom="tight">
        <Text size="micro" color="subtle">
          {switching ? 'Signing you in to that tenant.' : 'Switching signs you in again.'}
        </Text>
      </Stack>
    </Stack>
  );
}
