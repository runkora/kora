'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Checkbox,
  DataTable,
  Icon,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog } from '@orbit/components';
import { ApiRoute } from '@orbit/lib/routing';
import { fmtBudget } from '@orbit/utils';
import type { QuotaGroup } from '../../_services/quota-server';
import type { PrincipalUsageRow } from '../../../model-gateway/_types';

interface QuotaGroupsTabProps {
  initialGroups: QuotaGroup[];
  onGroupsChange: (groups: QuotaGroup[]) => void;
  canWrite: boolean;
}

// ── Create group modal ────────────────────────────────────────────────────────

interface CreateGroupModalProps {
  open: boolean;
  onClose: () => void;
  onCreated: (group: QuotaGroup) => void;
}

function CreateGroupModal({ open, onClose, onCreated }: CreateGroupModalProps) {
  const [name, setName] = useState('');
  const [budget, setBudget] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    const budgetNum = parseFloat(budget);
    if (!name.trim()) {
      setError('Name is required.');
      return;
    }
    if (isNaN(budgetNum) || budgetNum <= 0) {
      setError('Monthly budget must be a positive number.');
      return;
    }

    setSaving(true);
    setError(null);
    try {
      const res = await fetch(ApiRoute.quotaGroups.POST(), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim(), monthly_budget_usd: budgetNum }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      const { data: created } = (await res.json()) as {
        data: {
          id: string;
          name: string;
          monthly_budget_usd: number;
          tenant_id: string;
          created_at: string;
          updated_at: string;
        };
      };
      onCreated(created);
      setName('');
      setBudget('');
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create group.');
    } finally {
      setSaving(false);
    }
  }, [name, budget, onCreated, onClose]);

  return (
    <Modal open={open} onClose={onClose} size="sm">
      <ModalHeader title="Create quota group" />
      <ModalBody>
        <Stack gap="comfortable">
          <Stack gap="snug">
            <Text size="supporting" color="muted">
              Name
            </Text>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. engineering"
              disabled={saving}
            />
          </Stack>
          <Stack gap="snug">
            <Text size="supporting" color="muted">
              Monthly budget (USD)
            </Text>
            <Input
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
              placeholder="e.g. 100"
              type="number"
              min="0.01"
              step="0.01"
              disabled={saving}
            />
          </Stack>
          {error && <Alert variant="error">{error}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button variant="default" size="sm" onClick={() => void handleSubmit()} disabled={saving}>
          {saving ? 'Creating…' : 'Create group'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}

// ── Members modal ─────────────────────────────────────────────────────────────

interface MembersModalProps {
  group: QuotaGroup;
  onClose: () => void;
}

function MembersModal({ group, onClose }: MembersModalProps) {
  const [principals, setPrincipals] = useState<PrincipalUsageRow[]>([]);
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [pending, setPending] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load principals and current members in parallel.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      setLoading(true);
      setError(null);
      try {
        const [pRes, mRes] = await Promise.all([
          fetch(ApiRoute.analyticsPrincipals.GET({ days: 30, limit: 200 })),
          fetch(ApiRoute.quotaGroupMembers.GET(group.id)),
        ]);
        if (!pRes.ok) throw new Error(`Failed to load principals (${pRes.status})`);
        if (!mRes.ok) throw new Error(`Failed to load members (${mRes.status})`);
        const pData = (await pRes.json()) as { data: PrincipalUsageRow[] };
        const mData = (await mRes.json()) as { data: string[] };
        if (!cancelled) {
          setPrincipals(pData.data ?? []);
          setMemberIds(new Set(mData.data ?? []));
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load data.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [group.id]);

  const toggle = useCallback((principalId: string) => {
    setMemberIds((prev) => {
      const next = new Set(prev);
      if (next.has(principalId)) {
        next.delete(principalId);
      } else {
        next.add(principalId);
      }
      return next;
    });
  }, []);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    try {
      // Diff against what's actually pending on the server (captured at open).
      const toAdd = [...memberIds].filter((id) => !pending.has(id));
      const toRemove = [...pending].filter((id) => !memberIds.has(id));

      await Promise.all([
        ...toAdd.map((pid) =>
          fetch(ApiRoute.quotaGroupMember.PUT(group.id, pid), { method: 'PUT' }).then((r) => {
            if (!r.ok) throw new Error(`Failed to add ${pid} (${r.status})`);
          }),
        ),
        ...toRemove.map((pid) =>
          fetch(ApiRoute.quotaGroupMember.DELETE(group.id, pid), { method: 'DELETE' }).then(
            (r) => {
              if (!r.ok) throw new Error(`Failed to remove ${pid} (${r.status})`);
            },
          ),
        ),
      ]);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save members.');
    } finally {
      setSaving(false);
    }
  }, [group.id, memberIds, pending, onClose]);

  // Once loaded, snapshot the server state so we can diff on save.
  useEffect(() => {
    if (!loading) setPending(new Set(memberIds));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading]);

  const label = (p: PrincipalUsageRow) =>
    p.display_name ?? p.email ?? p.principal_id;

  return (
    <Modal open onClose={onClose} size="sm">
      <ModalHeader title={`Members — ${group.name}`} />
      <ModalBody>
        <Stack gap="snug">
          {loading && (
            <Text size="supporting" color="muted">
              Loading…
            </Text>
          )}
          {!loading && principals.length === 0 && (
            <Text size="supporting" color="muted">
              No principals with inference activity in the last 30 days.
            </Text>
          )}
          {!loading &&
            principals.map((p) => (
              <Checkbox
                key={p.principal_id}
                checked={memberIds.has(p.principal_id)}
                onChange={() => toggle(p.principal_id)}
                disabled={saving}
                id={`member-${p.principal_id}`}
              >
                <Stack gap="none">
                  <Text size="supporting" color="foreground">
                    {label(p)}
                  </Text>
                  {p.display_name && (
                    <Text size="caption" color="subtle" mono>
                      {p.email ?? p.principal_id}
                    </Text>
                  )}
                </Stack>
              </Checkbox>
            ))}
          {error && <Alert variant="error">{error}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button
          variant="default"
          size="sm"
          onClick={() => void handleSave()}
          disabled={saving || loading}
        >
          {saving ? 'Saving…' : 'Save members'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function QuotaGroupsTab({ initialGroups, onGroupsChange, canWrite }: QuotaGroupsTabProps) {
  const [groups, setGroups] = useState<QuotaGroup[]>(initialGroups);
  const [createOpen, setCreateOpen] = useState(false);
  const [membersGroup, setMembersGroup] = useState<QuotaGroup | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<QuotaGroup | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const updateGroups = useCallback(
    (next: QuotaGroup[]) => {
      setGroups(next);
      onGroupsChange(next);
    },
    [onGroupsChange],
  );

  const handleCreated = useCallback(
    (group: QuotaGroup) => {
      updateGroups([...groups, group]);
    },
    [groups, updateGroups],
  );

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      const res = await fetch(ApiRoute.quotaGroup.DELETE(deleteTarget.id), { method: 'DELETE' });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      updateGroups(groups.filter((g) => g.id !== deleteTarget.id));
      setDeleteTarget(null);
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : 'Failed to delete group.');
    } finally {
      setDeleting(false);
    }
  }, [deleteTarget, groups, updateGroups]);

  const COLUMNS: DataTableColumn<QuotaGroup>[] = [
    {
      id: 'name',
      header: 'Group name',
      locked: true,
      cell: (row) => (
        <Text size="supporting" color="foreground">
          {row.name}
        </Text>
      ),
    },
    {
      id: 'budget',
      header: 'Monthly budget',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.monthly_budget_usd,
      cell: (row) => (
        <Text size="supporting" color="foreground" mono align="end">
          {fmtBudget(row.monthly_budget_usd)}
        </Text>
      ),
    },
    ...(canWrite
      ? [
          {
            id: 'actions',
            header: '',
            align: 'right' as const,
            cell: (row: QuotaGroup) => (
              <Stack direction="horizontal" justify="end">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setDeleteTarget(row);
                    setDeleteError(null);
                  }}
                >
                  Delete
                </Button>
              </Stack>
            ),
          },
        ]
      : []),
  ];

  return (
    <Stack gap="comfortable">
      <DataTable
        data={groups}
        columns={COLUMNS}
        getRowKey={(row) => row.id}
        noun="groups"
        searchPlaceholder="Search groups…"
        searchFn={(row, q) => row.name.toLowerCase().includes(q)}
        defaultSortId="budget"
        defaultSortDir="desc"
        emptyMessage="No quota groups yet."
        filteredEmptyMessage="No groups match your search."
        onRowClick={canWrite ? (row) => setMembersGroup(row) : undefined}
        toolbarActions={
          canWrite ? (
            <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
              <Icon name="plus" size="2xs" />
              New group
            </Button>
          ) : undefined
        }
      />

      <CreateGroupModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={handleCreated}
      />

      {membersGroup && (
        <MembersModal group={membersGroup} onClose={() => setMembersGroup(null)} />
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete quota group"
        description={`Delete "${deleteTarget?.name}"? All principals currently assigned to this group will have their group quota removed. This cannot be undone.`}
        confirmLabel="Delete group"
        onConfirm={() => void handleDelete()}
        confirming={deleting}
        error={deleteError}
      />
    </Stack>
  );
}
