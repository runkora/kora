'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  DataTable,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Progress,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog } from '@orbit/components';
import { ApiRoute } from '@orbit/lib/routing';
import { fmtCost, fmtBudget } from '@orbit/utils';
import type { QuotaRole } from '../../_services/quota-server';
import type { PrincipalUsageRow } from '../../../model-gateway/_types';

// ── Types ─────────────────────────────────────────────────────────────────────

interface PrincipalQuotaRow extends PrincipalUsageRow {
  monthly_budget_usd: number | null;
  quota_source: 'individual' | 'role' | 'none';
  role_name: string | null;
}

// ── Set quota modal ───────────────────────────────────────────────────────────

interface SetQuotaModalProps {
  row: PrincipalQuotaRow;
  roles: QuotaRole[];
  onClose: () => void;
  onSaved: () => void;
}

function SetQuotaModal({ row, roles, onClose, onSaved }: SetQuotaModalProps) {
  const [mode, setMode] = useState<'individual' | 'role'>(
    row.quota_source === 'role' ? 'role' : 'individual',
  );
  const [budget, setBudget] = useState(
    row.quota_source === 'individual' && row.monthly_budget_usd != null
      ? String(row.monthly_budget_usd)
      : '',
  );
  const [roleId, setRoleId] = useState('');
  const [saving, setSaving] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showClearConfirm, setShowClearConfirm] = useState(false);

  const canClear = row.quota_source === 'individual';

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    try {
      if (mode === 'individual') {
        const budgetNum = parseFloat(budget);
        if (isNaN(budgetNum) || budgetNum <= 0) {
          setError('Monthly budget must be a positive number.');
          setSaving(false);
          return;
        }
        const res = await fetch(ApiRoute.quotaPrincipal.PUT(row.principal_id), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ monthly_budget_usd: budgetNum }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
      } else {
        if (!roleId) {
          setError('Please select a role.');
          setSaving(false);
          return;
        }
        const res = await fetch(ApiRoute.quotaRoleMember.PUT(roleId, row.principal_id), {
          method: 'PUT',
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
      }
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save quota.');
    } finally {
      setSaving(false);
    }
  }, [mode, budget, roleId, row.principal_id, onSaved, onClose]);

  const handleClear = useCallback(async () => {
    setClearing(true);
    setError(null);
    try {
      const res = await fetch(ApiRoute.quotaPrincipal.DELETE(row.principal_id), {
        method: 'DELETE',
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to clear quota.');
    } finally {
      setClearing(false);
    }
  }, [row.principal_id, onSaved, onClose]);

  const displayName = row.display_name ?? row.email ?? row.principal_id;

  return (
    <>
      <Modal open onClose={onClose} size="sm">
        <ModalHeader title="Set spending limit" description={displayName} />
        <ModalBody>
          <Stack gap="comfortable">
            <Stack gap="snug">
              <Text size="supporting" color="muted">
                Source
              </Text>
              <NativeSelect
                value={mode}
                onChange={(e) => setMode(e.target.value as 'individual' | 'role')}
                disabled={saving || clearing}
              >
                <option value="individual">Individual</option>
                <option value="role">Role</option>
              </NativeSelect>
            </Stack>

            {mode === 'individual' ? (
              <Stack gap="snug">
                <Text size="supporting" color="muted">
                  Monthly budget (USD)
                </Text>
                <Input
                  value={budget}
                  onChange={(e) => setBudget(e.target.value)}
                  placeholder="e.g. 50"
                  type="number"
                  min="1"
                  step="1"
                  disabled={saving || clearing}
                />
              </Stack>
            ) : (
              <Stack gap="snug">
                <Text size="supporting" color="muted">
                  Role
                </Text>
                <NativeSelect
                  value={roleId}
                  onChange={(e) => setRoleId(e.target.value)}
                  disabled={saving || clearing || roles.length === 0}
                >
                  <option value="">Select a role…</option>
                  {roles.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </NativeSelect>
              </Stack>
            )}

            {error && <Alert variant="error">{error}</Alert>}
          </Stack>
        </ModalBody>
        <ModalFooter>
          <Stack direction="horizontal" justify="between" grow>
            {canClear ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowClearConfirm(true)}
                disabled={saving || clearing}
              >
                Clear limit
              </Button>
            ) : (
              <Stack />
            )}
            <Stack direction="horizontal" gap="snug">
              <Button variant="outline" size="sm" onClick={onClose} disabled={saving || clearing}>
                Cancel
              </Button>
              <Button
                variant="default"
                size="sm"
                onClick={() => void handleSave()}
                disabled={saving || clearing}
              >
                {saving ? 'Saving…' : 'Save limit'}
              </Button>
            </Stack>
          </Stack>
        </ModalFooter>
      </Modal>

      <ConfirmDialog
        open={showClearConfirm}
        onClose={() => setShowClearConfirm(false)}
        title="Remove spending limit"
        description={`Remove the individual spending limit for ${displayName}? They will fall back to any role quota, or have no limit.`}
        confirmLabel="Remove limit"
        onConfirm={() => void handleClear()}
        confirming={clearing}
        error={error ?? undefined}
      />
    </>
  );
}

// ── Hook — loads spend data ───────────────────────────────────────────────────

// Canonical seed record for a principal before quota/spend data is joined in.
interface PrincipalSeed {
  id: string;
  principal_type: string;
  display_name: string;
  email: string | null;
}

function usePrincipalsWithQuotas(roles: QuotaRole[]) {
  const [rows, setRows] = useState<PrincipalQuotaRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // Fetch all principal sources, group members, and analytics spend in parallel.
      // Analytics is best-effort — a 403 or failure just means spend shows as $0.
      //
      // Principal sources unioned here:
      //   - HUMAN  → GET /api/identity/users
      //   - AGENT  → GET /api/agents/types
      //
      // TODO: add SERVICE and SYSTEM principal sources here once those principal
      // types are implemented in the identity/credential-broker layer.
      //
      // PAT principals are intentionally excluded — they are extensions of the
      // HUMAN principals that created them; spend is attributed to the human.
      const [usersRes, agentTypesRes, analyticsRes, ...memberResponses] = await Promise.all([
        fetch(ApiRoute.identityUsers.GET()),
        fetch(ApiRoute.agentTypes.list()),
        fetch(ApiRoute.analyticsPrincipals.GET({ days: 30, limit: 500 })),
        ...roles.map((g) => fetch(ApiRoute.quotaRoleMembers.GET(g.id))),
      ]);

      if (!usersRes.ok) throw new Error(`Failed to load users (${usersRes.status})`);
      if (!agentTypesRes.ok)
        throw new Error(`Failed to load agent types (${agentTypesRes.status})`);

      const usersData = (await usersRes.json()) as {
        data: { id: string; email: string; display_name: string }[];
      };
      const agentTypesData = (await agentTypesRes.json()) as {
        data: { id: string; name: string; slug: string }[];
      };

      // Union all principal seeds, deduplicating by id.
      const seedMap = new Map<string, PrincipalSeed>();
      for (const u of usersData.data ?? []) {
        seedMap.set(u.id, {
          id: u.id,
          principal_type: 'HUMAN',
          display_name: u.display_name ?? u.email,
          email: u.email,
        });
      }
      for (const a of agentTypesData.data ?? []) {
        seedMap.set(a.id, {
          id: a.id,
          principal_type: 'AGENT',
          display_name: a.name,
          email: null,
        });
      }

      // Build spend map from analytics (best-effort — ignored on failure).
      const spendMap = new Map<string, number>();
      if (analyticsRes.ok) {
        const analyticsData = (await analyticsRes.json()) as { data: PrincipalUsageRow[] };
        for (const row of analyticsData.data ?? []) {
          spendMap.set(row.principal_id, row.estimated_cost_usd);
        }
      }

      // Build principalId → QuotaRole lookup from membership lists.
      const membershipMap = new Map<string, QuotaRole>();
      await Promise.all(
        roles.map(async (g, i) => {
          const res = memberResponses[i];
          if (!res?.ok) return;
          const body = (await res.json()) as { data: string[] };
          for (const pid of body.data ?? []) {
            membershipMap.set(pid, g);
          }
        }),
      );

      // Enrich each principal with individual quota or group membership.
      const enriched = await Promise.all(
        Array.from(seedMap.values()).map(async (seed): Promise<PrincipalQuotaRow> => {
          const base: PrincipalUsageRow = {
            principal_id: seed.id,
            principal_type: seed.principal_type,
            request_count: 0,
            total_tokens: 0,
            prompt_tokens: 0,
            completion_tokens: 0,
            cache_read_tokens: 0,
            cache_creation_tokens: 0,
            estimated_cost_usd: spendMap.get(seed.id) ?? 0,
            error_rate: 0,
            avg_latency_ms: 0,
            last_active_at: '',
            email: seed.email,
            display_name: seed.display_name,
          };

          try {
            const qRes = await fetch(ApiRoute.quotaPrincipal.GET(seed.id));
            if (qRes.ok) {
              const q = (await qRes.json()) as { data: { monthly_budget_usd: number } };
              return {
                ...base,
                monthly_budget_usd: q.data.monthly_budget_usd,
                quota_source: 'individual',
                role_name: null,
              };
            }
          } catch {
            // fall through to group lookup
          }

          const grp = membershipMap.get(seed.id);
          if (grp) {
            return {
              ...base,
              monthly_budget_usd: grp.monthly_budget_usd,
              quota_source: 'role',
              role_name: grp.name,
            };
          }

          return { ...base, monthly_budget_usd: null, quota_source: 'none', role_name: null };
        }),
      );

      setRows(enriched);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }, [roles]);

  useEffect(() => {
    void load();
  }, [load]);

  return { rows, loading, error, reload: load };
}

// ── Filter options ────────────────────────────────────────────────────────────

const TYPE_FILTER_OPTIONS = [
  { value: 'HUMAN', label: 'Human' },
  { value: 'AGENT', label: 'Agent' },
  { value: 'SERVICE', label: 'Service' },
];

// ── Main component ────────────────────────────────────────────────────────────

interface QuotaPrincipalsTabProps {
  roles: QuotaRole[];
  canWrite: boolean;
}

export function QuotaPrincipalsTab({ roles, canWrite }: QuotaPrincipalsTabProps) {
  const { rows, loading, error, reload } = usePrincipalsWithQuotas(roles);
  const [modalRow, setModalRow] = useState<PrincipalQuotaRow | null>(null);

  const utilisationVariant = (pct: number) => {
    if (pct >= 100) return 'error' as const;
    if (pct >= 80) return 'degraded' as const;
    return 'healthy' as const;
  };

  const COLUMNS: DataTableColumn<PrincipalQuotaRow>[] = [
    {
      id: 'user',
      header: 'User',
      locked: true,
      cell: (row) => (
        <Stack gap="none">
          <Text size="supporting" color="foreground">
            {row.display_name ?? row.email ?? row.principal_id}
          </Text>
          {(row.display_name ?? row.email) && (
            <Text size="caption" color="subtle" mono>
              {row.email ?? row.principal_id}
            </Text>
          )}
        </Stack>
      ),
    },
    {
      id: 'type',
      header: 'Type',
      sortable: true,
      sortAccessor: (row) => row.principal_type ?? '',
      filter: {
        mode: 'client',
        options: TYPE_FILTER_OPTIONS,
        filterFn: (row, selected) => selected.has(row.principal_type?.toUpperCase() ?? ''),
      },
      cell: (row) =>
        row.principal_type ? (
          <Badge variant="secondary" size="sm">
            {row.principal_type}
          </Badge>
        ) : (
          <Text size="supporting" color="subtle">
            —
          </Text>
        ),
    },
    {
      id: 'spend',
      header: 'Month spend',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.estimated_cost_usd,
      cell: (row) => (
        <Text size="supporting" color="foreground" mono align="end">
          {fmtCost(row.estimated_cost_usd)}
        </Text>
      ),
    },
    {
      id: 'budget',
      header: 'Budget',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.monthly_budget_usd ?? Infinity,
      cell: (row) =>
        row.monthly_budget_usd != null ? (
          <Text size="supporting" color="foreground" mono align="end">
            {fmtBudget(row.monthly_budget_usd)}
          </Text>
        ) : (
          <Text size="supporting" color="subtle" align="end">
            —
          </Text>
        ),
    },
    {
      id: 'utilisation',
      header: 'Utilisation',
      cell: (row) => {
        if (row.monthly_budget_usd == null || row.monthly_budget_usd === 0) {
          return (
            <Text size="supporting" color="subtle">
              No limit
            </Text>
          );
        }
        const pct = (row.estimated_cost_usd / row.monthly_budget_usd) * 100;
        return (
          <Stack gap="tight">
            <Progress
              value={pct}
              max={100}
              label={`${row.display_name ?? row.principal_id} utilisation`}
              variant={utilisationVariant(pct)}
              showValue
              size="sm"
            />
          </Stack>
        );
      },
    },
    {
      id: 'source',
      header: 'Source',
      cell: (row) => {
        if (row.quota_source === 'none')
          return (
            <Badge variant="secondary" size="sm">
              None
            </Badge>
          );
        if (row.quota_source === 'role')
          return (
            <Badge variant="secondary" size="sm">
              {row.role_name ?? 'Role'}
            </Badge>
          );
        return (
          <Badge variant="secondary" size="sm">
            Individual
          </Badge>
        );
      },
    },
  ];

  return (
    <Stack gap="comfortable">
      {error && (
        <Text size="supporting" color="status-error">
          {error}
        </Text>
      )}

      <DataTable
        data={rows}
        columns={COLUMNS}
        getRowKey={(row) => row.principal_id}
        noun="principals"
        searchPlaceholder="Search email or user ID…"
        searchFn={(row, q) =>
          row.principal_id.toLowerCase().includes(q) ||
          (row.email?.toLowerCase().includes(q) ?? false) ||
          (row.display_name?.toLowerCase().includes(q) ?? false)
        }
        defaultSortId="spend"
        defaultSortDir="desc"
        emptyMessage={loading ? 'Loading…' : 'No inference activity in the last 30 days.'}
        filteredEmptyMessage="No principals match your search."
        onRowClick={canWrite ? (row) => setModalRow(row) : undefined}
      />

      {modalRow && (
        <SetQuotaModal
          row={modalRow}
          roles={roles}
          onClose={() => setModalRow(null)}
          onSaved={() => void reload()}
        />
      )}
    </Stack>
  );
}
