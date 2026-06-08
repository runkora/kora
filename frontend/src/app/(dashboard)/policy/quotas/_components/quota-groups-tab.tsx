'use client';

import { useCallback, useState } from 'react';
import {
  Alert,
  Button,
  DataTable,
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
import { fmtCost } from '@orbit/utils';
import type { QuotaGroup } from '../../_services/quota-server';

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
      const created = (await res.json()) as {
        id: string;
        name: string;
        monthly_budget_usd: number;
        tenant_id: string;
        created_at: string;
        updated_at: string;
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

// ── Main component ────────────────────────────────────────────────────────────

export function QuotaGroupsTab({ initialGroups, onGroupsChange, canWrite }: QuotaGroupsTabProps) {
  const [groups, setGroups] = useState<QuotaGroup[]>(initialGroups);
  const [createOpen, setCreateOpen] = useState(false);
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
          {fmtCost(row.monthly_budget_usd)}
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
        toolbarActions={
          canWrite ? (
            <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
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
