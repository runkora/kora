/** @tier 3 — sign-in-callback-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { Button, Card, Icon, Input, Stack, Text } from '@runkora/hades';
import { AuthCardShell, NO_TENANTS_MATCH, useTenantSearch } from '@runkora/components';
import type { SessionTenant } from '@runkora/server/auth-config';

export interface TenantPickerProps {
  /** The tenants the person's claims matched. */
  tenants: SessionTenant[];
  /** The tenant being signed in to, while the request runs. */
  pickingId?: string | null;
  onPick: (tenantId: string) => void;
}

/** Lists the matching tenants in the sign-in card when a sign-in matches more than one. */
export function TenantPicker({ tenants, pickingId, onPick }: TenantPickerProps) {
  const { query, setQuery, searchable, shown } = useTenantSearch(tenants);

  return (
    <AuthCardShell
      title="Choose a tenant"
      description="Your account can sign in to more than one tenant."
      footer={
        <Text size="micro" color="subtle">
          You can switch tenant later from the account menu.
        </Text>
      }
    >
      <Stack gap="snug">
        {searchable ? (
          <Input
            compact
            value={query}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setQuery(e.target.value)}
            placeholder="Search tenants"
            aria-label="Search tenants"
          />
        ) : null}
        <Card divide>
          {shown.map((t) => (
            <Button
              key={t.id}
              variant="ghost"
              shape="square"
              fullWidth
              disabled={Boolean(pickingId)}
              title={t.displayName}
              onClick={() => onPick(t.id)}
            >
              <Stack
                direction="horizontal"
                align="center"
                justify="between"
                gap="snug"
                grow
                minWidth="none"
              >
                <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
                  <Icon name="building" size="xs" color="subtle" />
                  <Text as="span" truncate>
                    {t.displayName}
                  </Text>
                </Stack>
                <Icon
                  name={pickingId === t.id ? 'loader' : 'chevron-right'}
                  size="xs"
                  color="subtle"
                />
              </Stack>
            </Button>
          ))}
        </Card>
        {shown.length === 0 ? (
          <Text size="caption" color="subtle">
            {NO_TENANTS_MATCH}
          </Text>
        ) : null}
      </Stack>
    </AuthCardShell>
  );
}
