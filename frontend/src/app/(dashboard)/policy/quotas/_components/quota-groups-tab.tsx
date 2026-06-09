'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
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

// ── Group modal (New + Edit) ───────────────────────────────────────────────────

interface GroupModalProps {
  /** Null = create mode; non-null = edit mode. */
  group: QuotaGroup | null;
  onClose: () => void;
  onSaved: (group: QuotaGroup) => void;
  onDeleted?: (id: string) => void;
}

const PRINCIPAL_TYPES = ['AGENT', 'HUMAN', 'SERVICE'] as const;
type PrincipalTypeKey = (typeof PRINCIPAL_TYPES)[number];

function typeLabel(t: string): string {
  if (t === 'HUMAN') return 'Human';
  if (t === 'AGENT') return 'Agent';
  if (t === 'SERVICE') return 'Service';
  return t;
}

function principalDisplayName(p: PrincipalUsageRow): string {
  return p.display_name ?? p.email ?? p.principal_id;
}

function GroupModal({ group, onClose, onSaved, onDeleted }: GroupModalProps) {
  const isEdit = group !== null;

  const [name, setName] = useState(group?.name ?? '');
  const [budget, setBudget] = useState(group ? String(group.monthly_budget_usd) : '');
  const [principalType, setPrincipalType] = useState<PrincipalTypeKey>('AGENT');
  const [principalSearch, setPrincipalSearch] = useState('');
  const [allPrincipals, setAllPrincipals] = useState<PrincipalUsageRow[]>([]);
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [serverMemberIds, setServerMemberIds] = useState<Set<string>>(new Set());
  const [loadingPrincipals, setLoadingPrincipals] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Load principals and (in edit mode) existing members.
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
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      setLoadingPrincipals(true);
      try {
        const requests: Promise<Response>[] = [
          fetch(ApiRoute.identityUsers.GET()),
          fetch(ApiRoute.agentTypes.list()),
        ];
        if (isEdit && group) {
          requests.push(fetch(ApiRoute.quotaGroupMembers.GET(group.id)));
        }
        const [usersRes, agentTypesRes, membersRes] = await Promise.all(requests);
        if (!usersRes!.ok) throw new Error(`Failed to load users (${usersRes!.status})`);
        if (!agentTypesRes!.ok)
          throw new Error(`Failed to load agent types (${agentTypesRes!.status})`);

        const usersData = (await usersRes!.json()) as {
          data: { id: string; email: string; display_name: string }[];
        };
        const agentTypesData = (await agentTypesRes!.json()) as {
          data: { id: string; name: string; slug: string }[];
        };

        const principals: PrincipalUsageRow[] = [
          ...(usersData.data ?? []).map(
            (u): PrincipalUsageRow => ({
              principal_id: u.id,
              principal_type: 'HUMAN',
              display_name: u.display_name ?? u.email,
              email: u.email,
              request_count: 0,
              total_tokens: 0,
              prompt_tokens: 0,
              completion_tokens: 0,
              cache_read_tokens: 0,
              cache_creation_tokens: 0,
              estimated_cost_usd: 0,
              error_rate: 0,
              avg_latency_ms: 0,
              last_active_at: '',
            }),
          ),
          ...(agentTypesData.data ?? []).map(
            (a): PrincipalUsageRow => ({
              principal_id: a.id,
              principal_type: 'AGENT',
              display_name: a.name,
              email: null,
              request_count: 0,
              total_tokens: 0,
              prompt_tokens: 0,
              completion_tokens: 0,
              cache_read_tokens: 0,
              cache_creation_tokens: 0,
              estimated_cost_usd: 0,
              error_rate: 0,
              avg_latency_ms: 0,
              last_active_at: '',
            }),
          ),
        ];

        const memberData = membersRes?.ok
          ? ((await membersRes.json()) as { data: string[] })
          : null;

        if (!cancelled) {
          setAllPrincipals(principals);
          const ids = new Set(memberData?.data ?? []);
          setMemberIds(ids);
          setServerMemberIds(ids);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load principals.');
      } finally {
        if (!cancelled) setLoadingPrincipals(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const toggleMember = useCallback((id: string) => {
    setMemberIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  // Principals for the currently-selected type, filtered by search.
  const visiblePrincipals = useMemo(() => {
    const forType = allPrincipals.filter(
      (p) => (p.principal_type?.toUpperCase() ?? '') === principalType,
    );
    const q = principalSearch.toLowerCase();
    if (!q) return forType;
    return forType.filter(
      (p) =>
        principalDisplayName(p).toLowerCase().includes(q) ||
        p.principal_id.toLowerCase().includes(q),
    );
  }, [allPrincipals, principalType, principalSearch]);

  const handleSave = useCallback(async () => {
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
      let saved: QuotaGroup;

      if (!isEdit) {
        const res = await fetch(ApiRoute.quotaGroups.POST(), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: name.trim(), monthly_budget_usd: budgetNum }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
        const { data: created } = (await res.json()) as { data: QuotaGroup };
        saved = created;

        await Promise.all(
          [...memberIds].map(async (pid) => {
            // Add to group and clear any individual quota in parallel — the
            // DELETE is a no-op (404) if no individual quota exists.
            await Promise.all([
              fetch(ApiRoute.quotaGroupMember.PUT(saved.id, pid), { method: 'PUT' }).then((r) => {
                if (!r.ok) throw new Error(`Failed to add member ${pid} (${r.status})`);
              }),
              fetch(ApiRoute.quotaPrincipal.DELETE(pid), { method: 'DELETE' }),
            ]);
          }),
        );
      } else {
        if (budgetNum !== group!.monthly_budget_usd) {
          const res = await fetch(ApiRoute.quotaGroup.PUT(group!.id), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ monthly_budget_usd: budgetNum }),
          });
          if (!res.ok) {
            const body = (await res.json().catch(() => ({}))) as { message?: string };
            throw new Error(body.message ?? `Request failed (${res.status})`);
          }
        }
        saved = { ...group!, name: name.trim(), monthly_budget_usd: budgetNum };

        const toAdd = [...memberIds].filter((id) => !serverMemberIds.has(id));
        const toRemove = [...serverMemberIds].filter((id) => !memberIds.has(id));
        await Promise.all([
          ...toAdd.map(async (pid) => {
            // Add to group and clear any individual quota in parallel — the
            // DELETE is a no-op (404) if no individual quota exists.
            await Promise.all([
              fetch(ApiRoute.quotaGroupMember.PUT(saved.id, pid), { method: 'PUT' }).then((r) => {
                if (!r.ok) throw new Error(`Failed to add member ${pid} (${r.status})`);
              }),
              fetch(ApiRoute.quotaPrincipal.DELETE(pid), { method: 'DELETE' }),
            ]);
          }),
          ...toRemove.map((pid) =>
            fetch(ApiRoute.quotaGroupMember.DELETE(saved.id, pid), { method: 'DELETE' }).then(
              (r) => {
                if (!r.ok) throw new Error(`Failed to remove member ${pid} (${r.status})`);
              },
            ),
          ),
        ]);
      }

      onSaved(saved);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save group.');
    } finally {
      setSaving(false);
    }
  }, [isEdit, name, budget, group, memberIds, serverMemberIds, onSaved, onClose]);

  const handleDelete = useCallback(async () => {
    if (!group) return;
    setDeleting(true);
    setError(null);
    try {
      const res = await fetch(ApiRoute.quotaGroup.DELETE(group.id), { method: 'DELETE' });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      onDeleted?.(group.id);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete group.');
    } finally {
      setDeleting(false);
    }
  }, [group, onDeleted, onClose]);

  const selectedCount = memberIds.size;

  return (
    <>
      <Modal open onClose={onClose} size="md">
        <ModalHeader title={isEdit ? 'Edit group' : 'New group'} />
        <ModalBody>
          <Stack gap="comfortable">
            {/* Name */}
            <Stack gap="snug">
              <Text size="supporting" color="muted">
                Name
              </Text>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. engineering"
                disabled={saving || deleting}
              />
            </Stack>

            {/* Budget */}
            <Stack gap="snug">
              <Text size="supporting" color="muted">
                Monthly budget (USD)
              </Text>
              <Input
                value={budget}
                onChange={(e) => setBudget(e.target.value)}
                placeholder="e.g. 100"
                type="number"
                min="1"
                step="1"
                disabled={saving || deleting}
              />
            </Stack>

            {/* Members */}
            <Stack gap="snug">
              <Text size="supporting" color="muted">
                Members ({selectedCount} selected)
              </Text>

              {/* Type toggle */}
              <Stack direction="horizontal" gap="tight">
                {PRINCIPAL_TYPES.map((t) => (
                  <Button
                    key={t}
                    variant={principalType === t ? 'default' : 'outline'}
                    size="sm"
                    onClick={() => {
                      setPrincipalType(t);
                      setPrincipalSearch('');
                    }}
                    disabled={saving || deleting}
                  >
                    {typeLabel(t)}
                  </Button>
                ))}
              </Stack>

              {/* Search */}
              <Input
                value={principalSearch}
                onChange={(e) => setPrincipalSearch(e.target.value)}
                placeholder={`Search ${typeLabel(principalType).toLowerCase()}s…`}
                disabled={saving || deleting}
              />

              {/* Principal list */}
              <Card maxHeight="scroll-sm" overflowY="auto" border="default" divide>
                {loadingPrincipals ? (
                  <Stack paddingBlock="tight" paddingInline="snug">
                    <Text size="caption" color="muted">
                      Loading…
                    </Text>
                  </Stack>
                ) : visiblePrincipals.length === 0 ? (
                  <Stack paddingBlock="tight" paddingInline="snug">
                    <Text size="caption" color="muted">
                      {principalSearch
                        ? 'No principals match your search.'
                        : `No ${typeLabel(principalType).toLowerCase()} principals found.`}
                    </Text>
                  </Stack>
                ) : (
                  visiblePrincipals.map((p) => {
                    const isSelected = memberIds.has(p.principal_id);
                    return (
                      <Button
                        key={p.principal_id}
                        variant="ghost"
                        size="tile"
                        selected={isSelected}
                        fullWidth
                        onClick={() => toggleMember(p.principal_id)}
                        disabled={saving || deleting}
                      >
                        <Stack
                          direction="horizontal"
                          grow
                          align="center"
                          justify="between"
                          minWidth="none"
                          paddingBlock="tight"
                          paddingInline="snug"
                        >
                          <Stack gap="none" minWidth="none">
                            <Text
                              size="micro"
                              truncate
                              color={isSelected ? 'accent' : 'foreground'}
                              weight={isSelected ? 'emphasis' : undefined}
                            >
                              {principalDisplayName(p)}
                            </Text>
                            {p.display_name && (
                              <Text size="micro" color="muted" mono truncate>
                                {p.email ?? p.principal_id}
                              </Text>
                            )}
                          </Stack>
                          {isSelected && <Icon name="check" size="2xs" color="accent" />}
                        </Stack>
                      </Button>
                    );
                  })
                )}
              </Card>
            </Stack>

            {error && <Alert variant="error">{error}</Alert>}
          </Stack>
        </ModalBody>
        <ModalFooter>
          <Stack direction="horizontal" justify="between" grow>
            {isEdit && onDeleted ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowDeleteConfirm(true)}
                disabled={saving || deleting}
              >
                Delete group
              </Button>
            ) : (
              <Stack />
            )}
            <Stack direction="horizontal" gap="snug">
              <Button variant="outline" size="sm" onClick={onClose} disabled={saving || deleting}>
                Cancel
              </Button>
              <Button
                variant="default"
                size="sm"
                onClick={() => void handleSave()}
                disabled={saving || deleting}
              >
                {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Create group'}
              </Button>
            </Stack>
          </Stack>
        </ModalFooter>
      </Modal>

      {isEdit && (
        <ConfirmDialog
          open={showDeleteConfirm}
          onClose={() => setShowDeleteConfirm(false)}
          title="Delete quota group"
          description={`Delete "${group?.name}"? All principals currently assigned to this group will have their group quota removed. This cannot be undone.`}
          confirmLabel="Delete group"
          onConfirm={() => void handleDelete()}
          confirming={deleting}
        />
      )}
    </>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function QuotaGroupsTab({ initialGroups, onGroupsChange, canWrite }: QuotaGroupsTabProps) {
  const [groups, setGroups] = useState<QuotaGroup[]>(initialGroups);
  // undefined = closed, null = create, QuotaGroup = edit
  const [modalGroup, setModalGroup] = useState<QuotaGroup | null | undefined>(undefined);

  const modalOpen = modalGroup !== undefined;

  const handleSaved = useCallback(
    (saved: QuotaGroup) => {
      setGroups((prev) => {
        const idx = prev.findIndex((g) => g.id === saved.id);
        const next = idx >= 0 ? prev.map((g) => (g.id === saved.id ? saved : g)) : [...prev, saved];
        onGroupsChange(next);
        return next;
      });
    },
    [onGroupsChange],
  );

  const handleDeleted = useCallback(
    (id: string) => {
      setGroups((prev) => {
        const next = prev.filter((g) => g.id !== id);
        onGroupsChange(next);
        return next;
      });
    },
    [onGroupsChange],
  );

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
        onRowClick={canWrite ? (row) => setModalGroup(row) : undefined}
        toolbarActions={
          canWrite ? (
            <Button variant="default" size="sm" onClick={() => setModalGroup(null)}>
              <Icon name="plus" size="2xs" />
              New group
            </Button>
          ) : undefined
        }
      />

      {modalOpen && (
        <GroupModal
          group={modalGroup ?? null}
          onClose={() => setModalGroup(undefined)}
          onSaved={handleSaved}
          onDeleted={canWrite ? handleDeleted : undefined}
        />
      )}
    </Stack>
  );
}
