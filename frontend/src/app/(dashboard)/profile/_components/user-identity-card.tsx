'use client';

/**
 * @tier 3 — profile-scoped. Promote to src/components/ if a second feature
 * needs a user identity card.
 */

import {
  CopyField,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Icon,
  Stack,
  Text,
} from '@clear-route/hades';
import type { IronSession } from 'iron-session';
import type { SessionPayload } from '@orbit/server/auth-config';

interface UserIdentityCardProps {
  session: IronSession<SessionPayload>;
  profile:
    | { ok: true; data: { id: string; email: string; display_name: string } }
    | { ok: false; status: number; message: string };
}

export function UserIdentityCard({ session, profile }: UserIdentityCardProps) {
  const displayName = profile.ok ? profile.data.display_name : session.user.name;
  const email = profile.ok ? profile.data.email : session.user.email;
  const principalId = session.user.sub;
  const tenantId = session.tenantId;

  return (
    <Card column>
      <CardHeader compact icon={<Icon name="user" />}>
        <CardTitle>User Identity</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="comfortable">
          {!profile.ok && (
            <Text size="caption" color="muted">
              Showing session data — identity service unavailable.
            </Text>
          )}
          <Stack gap="snug">
            <Text size="caption" weight="emphasis" color="subtle">
              Display Name
            </Text>
            <Text>{displayName}</Text>
          </Stack>
          <Stack gap="snug">
            <Text size="caption" weight="emphasis" color="subtle">
              Email
            </Text>
            <Text>{email}</Text>
          </Stack>
          <Stack gap="snug">
            <Text size="caption" weight="emphasis" color="subtle">
              Principal ID
            </Text>
            <CopyField
              value={principalId}
              ariaLabel="Copy principal ID"
              mono
              bordered={false}
              fullWidth={false}
            />
          </Stack>
          <Stack gap="snug">
            <Text size="caption" weight="emphasis" color="subtle">
              Tenant
            </Text>
            <Text mono size="supporting">
              {tenantId}
            </Text>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}
