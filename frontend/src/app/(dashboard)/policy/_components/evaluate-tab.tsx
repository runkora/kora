'use client';

import { useCallback, useMemo, useState } from 'react';
import {
  Callout,
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Heading,
  Input,
  NativeSelect,
  Separator,
  Stack,
  Text,
  Icon,
} from '@runkora/hades';
import type {
  CheckResponse,
  CheckSimulateRequest,
  PrincipalType,
  ScopesExplainResponse,
} from '../_types';
import { fetchPolicyScopesExplain, simulatePolicyCheck } from '../_services/policy-client';
import { PrincipalBadge, principalLabel, ScopeChip } from '@runkora/components';

interface EvaluateTabProps {
  initialExplain: ScopesExplainResponse | null;
  availableScopes: string[];
  availableRoles: string[];
  initialError: string | null;
}

function parseScope(scope: string): { resourceType: string; path: string[]; action: string } {
  const parts = scope.split(':');
  if (parts.length < 2) return { resourceType: scope, path: [], action: '' };
  return {
    resourceType: parts[0],
    path: parts.slice(1, -1),
    action: parts[parts.length - 1],
  };
}

export function EvaluateTab({
  initialExplain,
  availableScopes,
  availableRoles,
  initialError,
}: EvaluateTabProps) {
  // ── Lookup state ────────────────────────────────────────────────────────────
  const [lookupType, setLookupType] = useState<PrincipalType>(
    (initialExplain?.principal_type as PrincipalType) ?? 'AGENT',
  );
  const [lookupId, setLookupId] = useState(initialExplain?.principal_id ?? '');
  const [explain, setExplain] = useState(initialExplain);
  const [lookupError, setLookupError] = useState(initialError);
  const [lookupLoading, setLookupLoading] = useState(false);

  // ── Simulate state ──────────────────────────────────────────────────────────
  const [simPrincipalType, setSimPrincipalTypeRaw] = useState<PrincipalType>('AGENT');
  const [simPrincipalId, setSimPrincipalIdRaw] = useState('');
  const [simRole, setSimRoleRaw] = useState('');
  const [simScope, setSimScopeRaw] = useState('');
  const [simResult, setSimResult] = useState<CheckResponse | null>(null);
  const [simLoading, setSimLoading] = useState(false);
  const [simError, setSimError] = useState<string | null>(null);

  // Clear stale result whenever any simulation input changes.
  const clearSimResult = useCallback(() => {
    setSimResult(null);
    setSimError(null);
  }, []);
  const setSimPrincipalType = useCallback(
    (v: PrincipalType) => {
      setSimPrincipalTypeRaw(v);
      clearSimResult();
    },
    [clearSimResult],
  );
  const setSimPrincipalId = useCallback(
    (v: string) => {
      setSimPrincipalIdRaw(v);
      clearSimResult();
    },
    [clearSimResult],
  );
  const setSimRole = useCallback(
    (v: string) => {
      setSimRoleRaw(v);
      clearSimResult();
    },
    [clearSimResult],
  );
  const setSimScope = useCallback(
    (v: string) => {
      setSimScopeRaw(v);
      clearSimResult();
    },
    [clearSimResult],
  );

  const scopeGroups = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const s of availableScopes) {
      const rt = s.split(':')[0];
      if (!map.has(rt)) map.set(rt, []);
      map.get(rt)!.push(s);
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [availableScopes]);

  const handleLookup = useCallback(async () => {
    const trimmedId = lookupId.trim();
    if (!trimmedId) return;
    setLookupLoading(true);
    setLookupError(null);
    const result = await fetchPolicyScopesExplain(lookupType, trimmedId);
    if (result.data) {
      setExplain(result.data);
    } else {
      setExplain(null);
      setLookupError(result.error);
    }
    setLookupLoading(false);
  }, [lookupType, lookupId]);

  const handleSimulate = useCallback(async () => {
    const trimmedId = simPrincipalId.trim();
    if (!trimmedId || !simScope) return;
    setSimLoading(true);
    setSimError(null);
    setSimResult(null);
    const parsed = parseScope(simScope);
    const body: CheckSimulateRequest = {
      principal_type: simPrincipalType,
      principal_id: trimmedId,
      roles: simRole ? [simRole] : undefined,
      resource: {
        type: parsed.resourceType,
        path: parsed.path.length > 0 ? parsed.path : undefined,
      },
      action: parsed.action,
    };
    const result = await simulatePolicyCheck(body);
    if (result.data) {
      setSimResult(result.data);
    } else {
      setSimError(result.error);
    }
    setSimLoading(false);
  }, [simPrincipalType, simPrincipalId, simRole, simScope]);

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Evaluate</Heading>
        <Text size="supporting" color="muted">
          Inspect effective scopes and test policy decisions.
        </Text>
      </Stack>

      {/* Info box */}
      <Callout>
        <Stack gap="tight">
          <Text size="caption" color="muted">
            Uses the same OPA evaluation path as live enforcement.
          </Text>
          <Text size="caption" color="muted">
            <Text as="span" size="caption" color="foreground" weight="emphasis">
              Scope lookup
            </Text>{' '}
            shows the full permission set for a principal (your own session is pre-loaded).
          </Text>
          <Text size="caption" color="muted">
            <Text as="span" size="caption" color="foreground" weight="emphasis">
              Policy check
            </Text>{' '}
            dry-runs a specific operation against a principal with a chosen role.
          </Text>
        </Stack>
      </Callout>

      {/* ── Scope Lookup ───────────────────────────────────────────────────────── */}
      <Card column>
        <CardHeader compact icon={<Icon name="search" />}>
          <CardTitle>Scope lookup</CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="comfortable">
            <Text size="caption" color="muted">
              Shows the effective scope set for a principal: the union of scopes from OIDC role
              mappings and direct grants. Your own session is loaded by default. For other
              principals, only grant-based scopes are shown (OIDC roles require their session).
            </Text>
            <Stack direction="horizontal" align="end" gap="snug" wrap>
              <Stack gap="tight" minWidth="form-field-sm">
                <Text size="caption" weight="emphasis">
                  Type
                </Text>
                <NativeSelect
                  value={lookupType}
                  onChange={(e) => setLookupType(e.target.value as PrincipalType)}
                  aria-label="Principal type"
                >
                  <option value="AGENT">{principalLabel('AGENT')}</option>
                  <option value="HUMAN">{principalLabel('HUMAN')}</option>
                  <option value="SERVICE">{principalLabel('SERVICE')}</option>
                </NativeSelect>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field">
                <Text size="caption" weight="emphasis">
                  Principal ID
                </Text>
                <Input
                  id="scope-lookup-id"
                  type="text"
                  placeholder="e.g. agent-uuid or user@example.com"
                  value={lookupId}
                  onChange={(e) => setLookupId(e.target.value)}
                  compact
                />
              </Stack>
              <Button size="sm" onClick={handleLookup} disabled={lookupLoading || !lookupId}>
                {lookupLoading ? 'Looking up...' : 'Look up'}
              </Button>
            </Stack>

            {lookupError && (
              <Alert variant="error">
                <Icon name="triangle-alert" color="status-error" />
                <Text size="caption" color="status-error">
                  {lookupError}
                </Text>
              </Alert>
            )}

            {explain && (
              <Stack gap="comfortable">
                <Stack direction="horizontal" align="center" justify="between" gap="snug">
                  <Stack direction="horizontal" align="center" gap="snug">
                    <PrincipalBadge type={explain.principal_type as PrincipalType} />
                    <Text size="caption" mono color="muted">
                      {explain.principal_id}
                    </Text>
                  </Stack>
                  <Badge variant="secondary" size="sm">
                    {explain.tenant_id}
                  </Badge>
                </Stack>

                {explain.from_roles.length > 0 && (
                  <Stack gap="snug">
                    <Text size="caption" weight="emphasis" color="subtle">
                      From roles
                    </Text>
                    {explain.from_roles.map((r) => (
                      <Card key={r.role} subtle>
                        <CardContent padded compact>
                          <Stack gap="snug">
                            <Stack direction="horizontal" align="center" gap="snug">
                              <Text size="caption" mono color="accent">
                                {r.role}
                              </Text>
                              <Text size="caption" color="subtle">
                                → {r.scopes.length} {r.scopes.length === 1 ? 'scope' : 'scopes'}
                              </Text>
                            </Stack>
                            <Stack direction="horizontal" gap="tight" wrap>
                              {r.scopes.map((s) => (
                                <ScopeChip key={s} scope={s} />
                              ))}
                            </Stack>
                          </Stack>
                        </CardContent>
                      </Card>
                    ))}
                  </Stack>
                )}

                {explain.from_grants.length > 0 && (
                  <Stack gap="snug">
                    <Text size="caption" weight="emphasis" color="subtle">
                      From grants
                    </Text>
                    {explain.from_grants.map((g) => (
                      <Card key={g.grant_id}>
                        <CardContent padded>
                          <Stack gap="snug">
                            <Text size="caption" mono color="subtle">
                              {g.grant_id}
                            </Text>
                            <Stack direction="horizontal" gap="tight" wrap>
                              {g.scopes.map((s) => (
                                <ScopeChip key={s} scope={s} />
                              ))}
                            </Stack>
                          </Stack>
                        </CardContent>
                      </Card>
                    ))}
                  </Stack>
                )}

                {(explain.from_roles.length > 0 || explain.from_grants.length > 0) && <Separator />}

                <Stack gap="snug">
                  <Text size="caption" weight="emphasis" color="subtle">
                    Effective scope set ({explain.effective_scopes.length})
                  </Text>
                  {explain.effective_scopes.length === 0 ? (
                    <Card>
                      <CardContent padded>
                        <Stack gap="snug" align="center" paddingBlock="spacious">
                          <Icon name="shield-off" size="md" color="subtle" />
                          <Text size="caption" color="muted">
                            No effective scopes: this principal has no permissions.
                          </Text>
                        </Stack>
                      </CardContent>
                    </Card>
                  ) : (
                    <Stack direction="horizontal" gap="tight" wrap>
                      {explain.effective_scopes.map((s) => (
                        <ScopeChip key={s} scope={s} />
                      ))}
                    </Stack>
                  )}
                </Stack>
              </Stack>
            )}
          </Stack>
        </CardContent>
      </Card>

      {/* ── Policy Check ───────────────────────────────────────────────────────── */}
      <Card column>
        <CardHeader compact>
          <CardTitle>Policy check</CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="comfortable">
            <Text size="caption" color="muted">
              Dry-run a policy decision. Select a principal, assign them a role, and check whether a
              specific scope would be allowed. The check evaluates against the role&apos;s scope
              mappings plus any direct grants for the principal.
            </Text>

            {/* Principal row */}
            <Stack direction="horizontal" align="start" gap="snug" wrap>
              <Stack gap="tight" minWidth="form-field-sm">
                <Text size="caption" weight="emphasis">
                  Principal type
                </Text>
                <NativeSelect
                  value={simPrincipalType}
                  onChange={(e) => setSimPrincipalType(e.target.value as PrincipalType)}
                  aria-label="Principal type"
                >
                  <option value="AGENT">{principalLabel('AGENT')}</option>
                  <option value="HUMAN">{principalLabel('HUMAN')}</option>
                  <option value="SERVICE">{principalLabel('SERVICE')}</option>
                </NativeSelect>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field-sm">
                <Text size="caption" weight="emphasis">
                  Principal ID
                </Text>
                <Input
                  id="sim-principal-id"
                  type="text"
                  placeholder="e.g. agent-uuid or user@example.com"
                  value={simPrincipalId}
                  onChange={(e) => setSimPrincipalId(e.target.value)}
                  compact
                />
              </Stack>
            </Stack>

            {/* Role + scope row */}
            <Stack direction="horizontal" align="end" gap="snug" wrap>
              <Stack gap="tight" grow minWidth="form-field-sm">
                <Stack direction="horizontal" align="center" gap="tight">
                  <Text size="caption" weight="emphasis">
                    Role
                  </Text>
                  <Text size="caption" color="subtle">
                    (optional)
                  </Text>
                </Stack>
                <NativeSelect
                  value={simRole}
                  onChange={(e) => setSimRole(e.target.value)}
                  aria-label="Role"
                >
                  <option value="">No role (grants only)</option>
                  {availableRoles.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </NativeSelect>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field">
                <Text size="caption" weight="emphasis">
                  Scope to check
                </Text>
                <NativeSelect
                  value={simScope}
                  onChange={(e) => setSimScope(e.target.value)}
                  aria-label="Scope to check"
                  mono
                >
                  <option value="">Select a scope...</option>
                  {scopeGroups.map(([group, scopes]) => (
                    <optgroup key={group} label={group}>
                      {scopes.map((s) => (
                        <option key={s} value={s}>
                          {s}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </NativeSelect>
              </Stack>
              <Button
                size="sm"
                onClick={handleSimulate}
                disabled={simLoading || !simPrincipalId || !simScope}
              >
                {simLoading ? 'Checking...' : 'Run check'}
              </Button>
            </Stack>

            {simError && (
              <Alert variant="error">
                <Icon name="triangle-alert" color="status-error" />
                <Text size="caption" color="status-error">
                  {simError}
                </Text>
              </Alert>
            )}

            {simResult && (
              <Callout variant={simResult.allow ? 'success' : 'error'}>
                <Stack gap="snug">
                  <Stack direction="horizontal" align="center" gap="snug">
                    {simResult.allow ? (
                      <Icon name="check-circle" size="md" color="status-healthy" />
                    ) : (
                      <Icon name="x-circle" size="md" color="status-error" />
                    )}
                    <Text size="caption" weight="emphasis">
                      {simResult.allow ? 'ALLOW' : 'DENY'}
                    </Text>
                  </Stack>
                  <Stack direction="horizontal" align="center" gap="snug" wrap>
                    <Text size="caption" color="subtle">
                      {principalLabel(simPrincipalType)} {simPrincipalId}
                      {simRole ? ` as ${simRole}` : ''}
                      {' → '}
                    </Text>
                    <ScopeChip scope={simScope} />
                  </Stack>
                  <Text size="caption" color="muted">
                    {simResult.reason}
                  </Text>
                </Stack>
              </Callout>
            )}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
