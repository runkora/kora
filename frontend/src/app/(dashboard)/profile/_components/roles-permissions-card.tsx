/**
 * @tier 3 — profile-scoped. Promote to src/components/ if a second feature
 * needs a roles/permissions breakdown card.
 */

import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Grid,
  Icon,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import { ScopeList } from './scope-list';
import type { ScopesExplainResponse } from '../_types';

interface RolesPermissionsCardProps {
  explain:
    { ok: true; data: ScopesExplainResponse } | { ok: false; status: number; message: string };
  cachedScopes: string[];
  principalDisplayName: string;
}

export function RolesPermissionsCard({
  explain,
  cachedScopes,
  principalDisplayName,
}: RolesPermissionsCardProps) {
  return (
    <Card column>
      <CardHeader compact icon={<Icon name="shield" />}>
        <CardTitle>Roles & Permissions</CardTitle>
      </CardHeader>
      <CardContent>
        {!explain.ok ? (
          <Stack gap="comfortable">
            <Text size="caption" color="muted">
              Showing cached permissions — detailed breakdown unavailable.
            </Text>
            {cachedScopes.length === 0 ? (
              <Text size="caption" color="muted">
                No permissions found.
              </Text>
            ) : (
              <Stack gap="snug">
                <Text size="caption" weight="emphasis" color="subtle">
                  Cached scopes ({cachedScopes.length})
                </Text>
                <ScopeList scopes={cachedScopes} />
              </Stack>
            )}
          </Stack>
        ) : (
          <PermissionsBreakdown
            explain={explain.data}
            principalDisplayName={principalDisplayName}
          />
        )}
      </CardContent>
    </Card>
  );
}

// ── PermissionsBreakdown (success-path content) ──────────────────────────────

function PermissionsBreakdown({
  explain,
  principalDisplayName,
}: {
  explain: ScopesExplainResponse;
  principalDisplayName: string;
}) {
  const { from_roles, from_grants, effective_scopes } = explain;
  const hasGrantedBy = from_roles.length > 0 || from_grants.length > 0;

  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {hasGrantedBy && (
        <Card column>
          <CardHeader compact>
            <CardTitle>Granted by</CardTitle>
          </CardHeader>
          <CardContent>
            <Stack gap="comfortable">
              {from_roles.length > 0 && (
                <Stack gap="snug">
                  <Text size="caption" weight="emphasis" color="subtle">
                    From roles
                  </Text>
                  <Stack gap="comfortable">
                    {from_roles.map((r) => (
                      <Stack key={r.role} gap="snug">
                        <Stack direction="horizontal" align="center" gap="snug">
                          <Text size="caption" mono color="accent">
                            {r.role}
                          </Text>
                          <Text size="caption" color="subtle">
                            → {r.scopes.length} {r.scopes.length === 1 ? 'scope' : 'scopes'}
                          </Text>
                        </Stack>
                        <ScopeList scopes={r.scopes} />
                      </Stack>
                    ))}
                  </Stack>
                </Stack>
              )}

              {from_roles.length > 0 && from_grants.length > 0 && <Separator />}

              {from_grants.length > 0 && (
                <Stack gap="snug">
                  <Text size="caption" weight="emphasis" color="subtle">
                    From grants
                  </Text>
                  <Stack gap="comfortable">
                    {from_grants.map((g) => (
                      <Stack key={g.grant_id} gap="snug">
                        <Stack gap="tight">
                          <Text size="caption" weight="emphasis" color="accent">
                            {principalDisplayName}
                          </Text>
                          <Text size="caption" mono color="subtle">
                            {g.grant_id}
                          </Text>
                        </Stack>
                        <ScopeList scopes={g.scopes} />
                      </Stack>
                    ))}
                  </Stack>
                </Stack>
              )}
            </Stack>
          </CardContent>
        </Card>
      )}

      <Card column>
        <CardHeader compact>
          <CardTitle>Effective scope set ({effective_scopes.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {effective_scopes.length === 0 ? (
            <Text size="caption" color="muted">
              No effective scopes — this principal has no permissions.
            </Text>
          ) : (
            <ScopeList scopes={effective_scopes} />
          )}
        </CardContent>
      </Card>
    </Grid>
  );
}
