'use client';

import { useCallback, useMemo, useState } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Heading,
  Separator,
  Stack,
  Text,
  Icon,
} from '@orbit/terra';
import type {
  CheckResponse,
  CheckSimulateRequest,
  PrincipalType,
  ScopesExplainResponse,
} from '../_types';
import { fetchPolicyScopesExplain, simulatePolicyCheck } from '../_services/policy-client';
import { ScopeChip } from './scope-chip';
import { ErrorBanner } from './error-banner';

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
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Stack gap="tight">
          <Text size="caption" color="muted">
            Uses the same OPA evaluation path as live enforcement.
          </Text>
          <Text size="caption" color="muted">
            <Text asChild size="caption" color="foreground" weight="emphasis">
              <output>Scope lookup</output>
            </Text>{' '}
            shows the full permission set for a principal (your own session is pre-loaded).
          </Text>
          <Text size="caption" color="muted">
            <Text asChild size="caption" color="foreground" weight="emphasis">
              <output>Policy check</output>
            </Text>{' '}
            dry-runs a specific operation against a principal with a chosen role.
          </Text>
        </Stack>
      </section>

      {/* ── Scope Lookup ───────────────────────────────────────────────────────── */}
      <Card column>
        <CardHeader compact icon={<Icon name="search" />}>
          <CardTitle>Scope lookup</CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="comfortable">
            <Text size="caption" color="muted">
              Shows the effective scope set for a principal — the union of scopes from OIDC role
              mappings and direct grants. Your own session is loaded by default. For other
              principals, only grant-based scopes are shown (OIDC roles require their session).
            </Text>
            <Stack direction="horizontal" align="end" gap="snug" wrap>
              <Stack gap="tight">
                <Text size="caption" weight="emphasis">
                  Type
                </Text>
                <select
                  value={lookupType}
                  onChange={(e) => setLookupType(e.target.value as PrincipalType)}
                  aria-label="Principal type"
                  className="h-8 rounded-md border border-border bg-surface px-3 text-sm text-foreground"
                >
                  <option value="AGENT">AGENT</option>
                  <option value="HUMAN">HUMAN</option>
                  <option value="SERVICE">SERVICE</option>
                </select>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field">
                <Text size="caption" weight="emphasis">
                  Principal ID
                </Text>
                <input
                  type="text"
                  placeholder="e.g. agent-uuid or user@example.com"
                  value={lookupId}
                  onChange={(e) => setLookupId(e.target.value)}
                  className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                />
              </Stack>
              <Button size="sm" onClick={handleLookup} disabled={lookupLoading || !lookupId}>
                {lookupLoading ? 'Looking up...' : 'Look up'}
              </Button>
            </Stack>

            {lookupError && <ErrorBanner message={lookupError} />}

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
                      <section
                        key={r.role}
                        className="rounded-md border border-border-subtle bg-surface-elevated px-3 py-2.5"
                      >
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
                      </section>
                    ))}
                  </Stack>
                )}

                {explain.from_grants.length > 0 && (
                  <Stack gap="snug">
                    <Text size="caption" weight="emphasis" color="subtle">
                      From grants
                    </Text>
                    {explain.from_grants.map((g) => (
                      <section
                        key={g.grant_id}
                        className="rounded-md border border-border-subtle bg-surface-elevated px-3 py-2.5"
                      >
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
                      </section>
                    ))}
                  </Stack>
                )}

                {(explain.from_roles.length > 0 || explain.from_grants.length > 0) && <Separator />}

                <Stack gap="snug">
                  <Text size="caption" weight="emphasis" color="subtle">
                    Effective scope set ({explain.effective_scopes.length})
                  </Text>
                  {explain.effective_scopes.length === 0 ? (
                    <section className="rounded-md border border-border-subtle bg-surface-elevated px-4 py-6 text-center">
                      <Stack gap="snug" align="center">
                        <Icon name="shield-off" size="md" color="subtle" />
                        <Text size="caption" color="muted">
                          No effective scopes — this principal has no permissions.
                        </Text>
                      </Stack>
                    </section>
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
            <Stack direction="horizontal" align="end" gap="snug" wrap>
              <Stack gap="tight">
                <Text size="caption" weight="emphasis">
                  Principal type
                </Text>
                <select
                  value={simPrincipalType}
                  onChange={(e) => setSimPrincipalType(e.target.value as PrincipalType)}
                  aria-label="Principal type"
                  className="h-8 rounded-md border border-border bg-surface px-3 text-sm text-foreground"
                >
                  <option value="AGENT">AGENT</option>
                  <option value="HUMAN">HUMAN</option>
                  <option value="SERVICE">SERVICE</option>
                </select>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field-sm">
                <Text size="caption" weight="emphasis">
                  Principal ID
                </Text>
                <input
                  type="text"
                  placeholder="e.g. agent-uuid or user@example.com"
                  value={simPrincipalId}
                  onChange={(e) => setSimPrincipalId(e.target.value)}
                  className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
                <select
                  value={simRole}
                  onChange={(e) => setSimRole(e.target.value)}
                  aria-label="Role"
                  className="h-8 w-full rounded-md border border-border bg-surface px-3 text-sm text-foreground"
                >
                  <option value="">No role (grants only)</option>
                  {availableRoles.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
              </Stack>
              <Stack gap="tight" grow minWidth="form-field">
                <Text size="caption" weight="emphasis">
                  Scope to check
                </Text>
                <select
                  value={simScope}
                  onChange={(e) => setSimScope(e.target.value)}
                  aria-label="Scope to check"
                  className="h-8 w-full rounded-md border border-border bg-surface px-3 text-sm text-foreground font-mono"
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
                </select>
              </Stack>
              <Button
                size="sm"
                onClick={handleSimulate}
                disabled={simLoading || !simPrincipalId || !simScope}
              >
                {simLoading ? 'Checking...' : 'Run check'}
              </Button>
            </Stack>

            {simError && <ErrorBanner message={simError} />}

            {simResult && (
              <section
                className={`rounded-md border p-4 ${
                  simResult.allow
                    ? 'border-status-healthy/30 bg-status-healthy/5'
                    : 'border-status-error/30 bg-status-error/5'
                }`}
              >
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
                      {simPrincipalType} {simPrincipalId}
                      {simRole ? ` as ${simRole}` : ''}
                      {' → '}
                    </Text>
                    <ScopeChip scope={simScope} />
                  </Stack>
                  <Text size="caption" color="muted">
                    {simResult.reason}
                  </Text>
                </Stack>
              </section>
            )}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}

// ── PrincipalBadge ───────────────────────────────────────────────────────────

function PrincipalBadge({ type }: { type: PrincipalType }) {
  const variant = type === 'AGENT' ? 'default' : type === 'HUMAN' ? 'healthy' : 'degraded';
  return (
    <Badge variant={variant} size="sm">
      {type}
    </Badge>
  );
}
