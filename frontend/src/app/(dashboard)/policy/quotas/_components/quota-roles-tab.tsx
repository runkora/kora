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
import type { QuotaRole } from '../../_services/quota-server';
import type { PrincipalUsageRow } from '../../../model-gateway/_types';

interface QuotaRolesTabProps {
  initialRoles: QuotaRole[];
  onRolesChange: (roles: QuotaRole[]) => void;
  canWrite: boolean;
}

// ── Group modal (New + Edit) ───────────────────────────────────────────────────

interface RoleModalProps {
  /** Null = create mode; non-null = edit mode. */
  role: QuotaRole | null;
  onClose: () => void;
  onSaved: (role: QuotaRole) => void;
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

function RoleModal({ role, onClose, onSaved, onDeleted }: RoleModalProps) {
  const isEdit = role !== null;

  const [name, setName] = useState(role?.name ?? '');
  const [budget, setBudget] = useState(role ? String(role.monthly_budget_usd) : '');
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
        if (isEdit && role) {
          requests.push(fetch(ApiRoute.quotaRoleMembers.GET(role.id)));
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
      let saved: QuotaRole;

      if (!isEdit) {
        const res = await fetch(ApiRoute.quotaRoles.POST(), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: name.trim(), monthly_budget_usd: budgetNum }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string };
          throw new Error(body.message ?? `Request failed (${res.status})`);
        }
        const { data: created } = (await res.json()) as { data: QuotaRole };
        saved = created;

        await Promise.all(
          [...memberIds].map((pid) =>
            fetch(ApiRoute.quotaRoleMember.PUT(saved.id, pid), { method: 'PUT' }).then((r) => {
              if (!r.ok) throw new Error(`Failed to add member ${pid} (${r.status})`);
            }),
          ),
        );
      } else {
        if (budgetNum !== role!.monthly_budget_usd) {
          const res = await fetch(ApiRoute.quotaRole.PUT(role!.id), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ monthly_budget_usd: budgetNum }),
          });
          if (!res.ok) {
            const body = (await res.json().catch(() => ({}))) as { message?: string };
            throw new Error(body.message ?? `Request failed (${res.status})`);
          }
        }
        saved = { ...role!, name: name.trim(), monthly_budget_usd: budgetNum };

        const toAdd = [...memberIds].filter((id) => !serverMemberIds.has(id));
        const toRemove = [...serverMemberIds].filter((id) => !memberIds.has(id));
        await Promise.all([
          ...toAdd.map((pid) =>
            fetch(ApiRoute.quotaRoleMember.PUT(saved.id, pid), { method: 'PUT' }).then((r) => {
              if (!r.ok) throw new Error(`Failed to add member ${pid} (${r.status})`);
            }),
          ),
          ...toRemove.map((pid) =>
            fetch(ApiRoute.quotaRoleMember.DELETE(saved.id, pid), { method: 'DELETE' }).then(
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
      setError(err instanceof Error ? err.message : 'Failed to save role.');
    } finally {
      setSaving(false);
    }
  }, [isEdit, name, budget, role, memberIds, serverMemberIds, onSaved, onClose]);

  const handleDelete = useCallback(async () => {
    if (!role) return;
    setDeleting(true);
    setError(null);
    try {
      const res = await fetch(ApiRoute.quotaRole.DELETE(role.id), { method: 'DELETE' });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message ?? `Request failed (${res.status})`);
      }
      onDeleted?.(role.id);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete role.');
    } finally {
      setDeleting(false);
    }
  }, [role, onDeleted, onClose]);

  const selectedCount = memberIds.size;

  return (
    <>
      <Modal open onClose={onClose} size="md">
        <ModalHeader title={isEdit ? 'Edit role' : 'New role'} />
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
                Delete role
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
                {saving ? 'Saving…' : isEdit ? 'Save changes' : 'Create role'}
              </Button>
            </Stack>
          </Stack>
        </ModalFooter>
      </Modal>

      {isEdit && (
        <ConfirmDialog
          open={showDeleteConfirm}
          onClose={() => setShowDeleteConfirm(false)}
          title="Delete quota role"
          description={`Delete "${role?.name}"? All principals currently assigned to this role will have their role quota removed. This cannot be undone.`}
          confirmLabel="Delete role"
          onConfirm={() => void handleDelete()}
          confirming={deleting}
        />
      )}
    </>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function QuotaRolesTab({ initialRoles, onRolesChange, canWrite }: QuotaRolesTabProps) {
  const [roles, setRoles] = useState<QuotaRole[]>(initialRoles);
  // undefined = closed, null = create, QuotaRole = edit
  const [modalRole, setModalRole] = useState<QuotaRole | null | undefined>(undefined);

  const modalOpen = modalRole !== undefined;

  const handleSaved = useCallback(
    (saved: QuotaRole) => {
      setRoles((prev) => {
        const idx = prev.findIndex((g) => g.id === saved.id);
        const next = idx >= 0 ? prev.map((g) => (g.id === saved.id ? saved : g)) : [...prev, saved];
        onRolesChange(next);
        return next;
      });
    },
    [onRolesChange],
  );

  const handleDeleted = useCallback(
    (id: string) => {
      setRoles((prev) => {
        const next = prev.filter((g) => g.id !== id);
        onRolesChange(next);
        return next;
      });
    },
    [onRolesChange],
  );

  const COLUMNS: DataTableColumn<QuotaRole>[] = [
    {
      id: 'name',
      header: 'Role name',
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
        data={roles}
        columns={COLUMNS}
        getRowKey={(row) => row.id}
        noun="roles"
        searchPlaceholder="Search roles…"
        searchFn={(row, q) => row.name.toLowerCase().includes(q)}
        defaultSortId="budget"
        defaultSortDir="desc"
        emptyMessage="No quota roles yet."
        filteredEmptyMessage="No roles match your search."
        onRowClick={canWrite ? (row) => setModalRole(row) : undefined}
        toolbarActions={
          canWrite ? (
            <Button variant="default" size="sm" onClick={() => setModalRole(null)}>
              <Icon name="plus" size="2xs" />
              New role
            </Button>
          ) : undefined
        }
      />

      {modalOpen && (
        <RoleModal
          role={modalRole ?? null}
          onClose={() => setModalRole(undefined)}
          onSaved={handleSaved}
          onDeleted={canWrite ? handleDeleted : undefined}
        />
      )}
    </Stack>
  );
}
