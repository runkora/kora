'use client';

import { useCallback, useEffect, useState } from 'react';
import {
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
  Alert,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog } from '@orbit/components';
import { ApiRoute } from '@orbit/lib/routing';
import { fmtCost, fmtBudget } from '@orbit/utils';
import type { QuotaGroup } from '../../_services/quota-server';
import type { PrincipalUsageRow } from '../../../model-gateway/_types';

// ── Types ─────────────────────────────────────────────────────────────────────

interface PrincipalQuotaRow extends PrincipalUsageRow {
  monthly_budget_usd: number | null;
  quota_source: 'individual' | 'group' | 'none';
  group_name: string | null;
}

// ── Set quota modal ───────────────────────────────────────────────────────────

interface SetQuotaModalProps {
  principalId: string;
  groups: QuotaGroup[];
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
}

function SetQuotaModal({ principalId, groups, open, onClose, onSaved }: SetQuotaModalProps) {
  const [mode, setMode] = useState<'individual' | 'group'>('individual');
  const [budget, setBudget] = useState('');
  const [groupId, setGroupId] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
        const res = await fetch(ApiRoute.quotaPrincipal.PUT(principalId), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ monthly_budget_usd: budgetNum }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
      } else {
        if (!groupId) {
          setError('Please select a group.');
          setSaving(false);
          return;
        }
        const res = await fetch(ApiRoute.quotaGroupMember.PUT(groupId, principalId), {
          method: 'PUT',
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
      }
      setBudget('');
      setGroupId('');
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save quota.');
    } finally {
      setSaving(false);
    }
  }, [mode, budget, groupId, principalId, onSaved, onClose]);

  return (
    <Modal open={open} onClose={onClose} size="sm">
      <ModalHeader title="Set spending limit" />
      <ModalBody>
        <Stack gap="comfortable">
          <Stack gap="snug">
            <Text size="supporting" color="muted">
              Source
            </Text>
            <NativeSelect
              value={mode}
              onChange={(e) => setMode(e.target.value as 'individual' | 'group')}
              disabled={saving}
            >
              <option value="individual">Individual</option>
              <option value="group">Group</option>
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
                min="0.01"
                step="0.01"
                disabled={saving}
              />
            </Stack>
          ) : (
            <Stack gap="snug">
              <Text size="supporting" color="muted">
                Group
              </Text>
              <NativeSelect
                value={groupId}
                onChange={(e) => setGroupId(e.target.value)}
                disabled={saving || groups.length === 0}
              >
                <option value="">Select a group…</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name} ({fmtBudget(g.monthly_budget_usd)}/mo)
                  </option>
                ))}
              </NativeSelect>
            </Stack>
          )}

          {error && <Alert variant="error">{error}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button variant="default" size="sm" onClick={() => void handleSave()} disabled={saving}>
          {saving ? 'Saving…' : 'Save limit'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}

// ── Hook — loads spend data ───────────────────────────────────────────────────

function usePrincipalsWithQuotas() {
  const [rows, setRows] = useState<PrincipalQuotaRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // Fetch the principals spend data for last 30 days
      const res = await fetch(ApiRoute.analyticsPrincipals.GET({ days: 30, limit: 100 }));
      if (!res.ok) throw new Error(`Failed to load principals (${res.status})`);
      const data = (await res.json()) as { data: PrincipalUsageRow[] };

      // For each principal with spend, try to fetch their quota
      const enriched = await Promise.all(
        (data.data ?? []).map(async (p): Promise<PrincipalQuotaRow> => {
          try {
            const qRes = await fetch(ApiRoute.quotaPrincipal.GET(p.principal_id));
            if (qRes.status === 404) {
              return { ...p, monthly_budget_usd: null, quota_source: 'none', group_name: null };
            }
            if (!qRes.ok) throw new Error();
            const q = (await qRes.json()) as { data: { monthly_budget_usd: number } };
            return {
              ...p,
              monthly_budget_usd: q.data.monthly_budget_usd,
              quota_source: 'individual',
              group_name: null,
            };
          } catch {
            return { ...p, monthly_budget_usd: null, quota_source: 'none', group_name: null };
          }
        }),
      );

      setRows(enriched);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return { rows, loading, error, reload: load };
}

// ── Main component ────────────────────────────────────────────────────────────

interface QuotaPrincipalsTabProps {
  groups: QuotaGroup[];
  canWrite: boolean;
}

export function QuotaPrincipalsTab({ groups, canWrite }: QuotaPrincipalsTabProps) {
  const { rows, loading, error, reload } = usePrincipalsWithQuotas();
  const [setQuotaFor, setSetQuotaFor] = useState<string | null>(null);
  const [clearTarget, setClearTarget] = useState<PrincipalQuotaRow | null>(null);
  const [clearing, setClearing] = useState(false);
  const [clearError, setClearError] = useState<string | null>(null);

  const handleClear = useCallback(async () => {
    if (!clearTarget) return;
    setClearing(true);
    setClearError(null);
    try {
      const res = await fetch(ApiRoute.quotaPrincipal.DELETE(clearTarget.principal_id), {
        method: 'DELETE',
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      setClearTarget(null);
      await reload();
    } catch (err) {
      setClearError(err instanceof Error ? err.message : 'Failed to clear quota.');
    } finally {
      setClearing(false);
    }
  }, [clearTarget, reload]);

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
        if (row.quota_source === 'group')
          return (
            <Badge variant="secondary" size="sm">
              {row.group_name ?? 'Group'}
            </Badge>
          );
        return (
          <Badge variant="secondary" size="sm">
            Individual
          </Badge>
        );
      },
    },
    ...(canWrite
      ? [
          {
            id: 'actions',
            header: '',
            align: 'right' as const,
            cell: (row: PrincipalQuotaRow) => (
              <Stack direction="horizontal" gap="tight" justify="end">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSetQuotaFor(row.principal_id)}
                >
                  Set limit
                </Button>
                {row.quota_source === 'individual' && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setClearTarget(row);
                      setClearError(null);
                    }}
                  >
                    Clear
                  </Button>
                )}
              </Stack>
            ),
          },
        ]
      : []),
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
      />

      {setQuotaFor && (
        <SetQuotaModal
          principalId={setQuotaFor}
          groups={groups}
          open={true}
          onClose={() => setSetQuotaFor(null)}
          onSaved={() => void reload()}
        />
      )}

      <ConfirmDialog
        open={clearTarget !== null}
        onClose={() => setClearTarget(null)}
        title="Remove spending limit"
        description={`Remove the individual spending limit for ${clearTarget?.display_name ?? clearTarget?.email ?? clearTarget?.principal_id}? They will fall back to any group quota, or have no limit.`}
        confirmLabel="Remove limit"
        onConfirm={() => void handleClear()}
        confirming={clearing}
        error={clearError}
      />
    </Stack>
  );
}
