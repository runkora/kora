/** @tier 3 — sign-in-callback-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { Button, Card, CardContent, CardHeader, Heading, Icon, Stack, Text } from '@runkora/hades';
import type { SessionTenant } from '@runkora/server/auth-config';

export interface TenantPickerProps {
  /** The tenants the person's claims matched. */
  tenants: SessionTenant[];
  /** The tenant being signed in to, while the request runs. */
  pickingId?: string | null;
  onPick: (tenantId: string) => void;
}

/** Lists the matching tenants when a sign-in matches more than one. */
export function TenantPicker({ tenants, pickingId, onPick }: TenantPickerProps) {
  return (
    <Card>
      <CardHeader>
        <Heading variant="section">Choose a tenant</Heading>
      </CardHeader>
      <CardContent>
        <Stack gap="comfortable">
          <Text size="supporting" color="muted">
            Your account can sign in to more than one tenant.
          </Text>
          <Stack gap="snug">
            {tenants.map((t) => (
              <Button
                key={t.id}
                variant="outline"
                fullWidth
                disabled={Boolean(pickingId)}
                onClick={() => onPick(t.id)}
              >
                <Text as="span" truncate>
                  {t.displayName}
                </Text>
                {pickingId === t.id ? <Icon name="loader" size="xs" color="subtle" /> : null}
              </Button>
            ))}
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}
