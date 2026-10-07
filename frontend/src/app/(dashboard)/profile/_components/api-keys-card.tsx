/**
 * @tier 3 — profile-scoped. Promote to src/components/ if a second feature
 * needs an API keys listing card.
 */

import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Grid,
  Icon,
  Stack,
  Text,
  EmptyState,
} from '@orbit/terra';
import { StatusBadge } from '@orbit/components';
import { fmtExpiry, isExpired } from '@orbit/utils';
import type { ListPATsResult } from '@orbit/server/services/identity-server';
import { ScopeList } from './scope-list';

interface ApiKeysCardProps {
  tokens: ListPATsResult;
}

export function ApiKeysCard({ tokens }: ApiKeysCardProps) {
  return (
    <Card column>
      <CardHeader compact icon={<Icon name="key" />}>
        <CardTitle>API Keys</CardTitle>
      </CardHeader>
      <CardContent>
        {!tokens.ok ? (
          <EmptyState
            icon="triangle-alert"
            title="Unable to load API keys"
            description={tokens.message}
          />
        ) : tokens.tokens.length === 0 ? (
          <EmptyState
            icon="key"
            title="No API keys"
            description="You haven't created any API keys yet."
          />
        ) : (
          <Grid cols={{ base: 1, md: 2 }} gap="comfortable">
            {tokens.tokens.map((pat) => (
              <Card key={pat.id}>
                <CardContent padded>
                  <Stack gap="snug">
                    <Stack direction="horizontal" align="center" justify="between" gap="snug">
                      <Text size="caption" weight="emphasis">
                        {pat.name}
                      </Text>
                      <StatusBadge
                        status={isExpired(pat.expires_at) ? 'error' : 'healthy'}
                        label={isExpired(pat.expires_at) ? 'Expired' : 'Active'}
                      />
                    </Stack>
                    <Text size="caption" color="muted">
                      Expires: {fmtExpiry(pat.expires_at)}
                    </Text>
                    {pat.scopes.length > 0 && <ScopeList scopes={pat.scopes} />}
                  </Stack>
                </CardContent>
              </Card>
            ))}
          </Grid>
        )}
      </CardContent>
    </Card>
  );
}
