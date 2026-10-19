'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Callout,
  Alert,
  Badge,
  Button,
  ButtonGroup,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Eyebrow,
  Grid,
  Icon,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
  Toggle,
  ToggleRow,
  PageSectionHeader,
  EmptyState,
  ConfirmDialog,
} from '@runkora/hades';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import type { RoleEntry } from '../_types';
import { upsertRoleAction, deleteRoleAction } from '../_actions/roles';
import { usePermissions } from '../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import { ScopeChipList, scopeColumnFilter } from '@orbit/components';
import { ScopePicker } from '@orbit/components';
import { ViewToggle } from './view-toggle';

// ── Reserved roles (cannot be modified via UI) ───────────────────────────────

const RESERVED_ROLES = new Set(['platform-admin']);

// ── Props ────────────────────────────────────────────────────────────────────

interface RolesTabProps {
  initialRoles: RoleEntry[];
  availableScopes: string[];
  initialError: string | null;
}

// ── Component ────────────────────────────────────────────────────────────────

export function RolesTab({ initialRoles, availableScopes, initialError }: RolesTabProps) {
  const router = useRouter();
  const { scopes: sessionScopes } = usePermissions();
  const canWrite = hasScope(sessionScopes, 'platform:roles:write');
  const [roles, setRoles] = useState(initialRoles);
  const [error, setError] = useState(initialError);

  // Sync error state when parent re-renders with new error
  useEffect(() => {
    setError(initialError);
  }, [initialError]);
  const [search, setSearch] = useState('');
  const [viewMode, setViewMode] = useState<'cards' | 'list'>('cards');
  const [editingRole, setEditingRole] = useState<RoleEntry | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<RoleEntry | null>(null);
  const [deletingRole, setDeletingRole] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  useEffect(() => {
    setRoles(initialRoles);
  }, [initialRoles]);

  const filtered = useMemo(() => {
    if (!search) return roles;
    const q = search.toLowerCase();
    return roles.filter(
      (r) =>
        r.role.toLowerCase().includes(q) ||
        (r.scopes ?? []).some((s) => s.toLowerCase().includes(q)),
    );
  }, [roles, search]);

  const handleDeleteConfirm = useCallback(
    async (role: string) => {
      setDeletingRole(role);
      setDeleteError(null);
      const result = await deleteRoleAction(role);
      if (result.ok) {
        setDeleteCandidate(null);
        setDeletingRole(null);
        router.refresh();
      } else {
        setDeleteError(result.message);
        setDeletingRole(null);
      }
    },
    [router],
  );

  return (
    <Stack gap="comfortable">
      {/* Header */}
      <PageSectionHeader
        title="Roles"
        description="Map OIDC role names to platform scopes."
        action={
          canWrite ? (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Icon name="plus" />
              Create Role
            </Button>
          ) : undefined
        }
      />

      {/* Info box */}
      <Callout>
        <Text size="caption" color="muted">
          Roles map OIDC role claims to platform scopes. When a human authenticates, kora resolves
          the union of scopes from all matching roles.
        </Text>
      </Callout>

      {error && (
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Text size="caption" color="status-error">
            {error}
          </Text>
        </Alert>
      )}

      {/* Search + view toggle — card mode only; list mode toolbar is owned by DataTable */}
      {viewMode === 'cards' && (
        <Stack direction="horizontal" align="center" justify="between">
          <Stack asChild grow maxWidth="compact">
            <Input
              placeholder="Search roles..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </Stack>
          <ViewToggle viewMode={viewMode} onToggle={setViewMode} />
        </Stack>
      )}

      {/* Content */}
      {viewMode === 'cards' ? (
        filtered.length === 0 ? (
          search ? (
            <EmptyState
              icon="search"
              title="No matches"
              description="No roles match your search. Try a different query."
            />
          ) : (
            <EmptyState
              icon="user-check"
              title="No roles yet"
              description="Roles map OIDC role claims to platform scopes. Create a role to define what permissions a role name grants."
              action={
                canWrite ? (
                  <Button size="sm" onClick={() => setCreating(true)}>
                    <Icon name="plus" />
                    Create Role
                  </Button>
                ) : undefined
              }
            />
          )
        ) : (
          <RoleCardGrid
            roles={filtered}
            onEdit={canWrite ? setEditingRole : undefined}
            onDelete={canWrite ? setDeleteCandidate : undefined}
          />
        )
      ) : (
        <RoleTable
          roles={roles}
          onEdit={canWrite ? setEditingRole : undefined}
          onDelete={canWrite ? setDeleteCandidate : undefined}
          viewToggle={<ViewToggle viewMode={viewMode} onToggle={setViewMode} />}
        />
      )}

      {/* Delete confirmation modal */}
      <ConfirmDialog
        open={!!deleteCandidate}
        title="Delete Role"
        description={
          deleteCandidate
            ? `Remove the role "${deleteCandidate.role}" and all its scope mappings? This action cannot be undone.`
            : ''
        }
        confirmLabel="Delete"
        size="md"
        confirming={deletingRole === deleteCandidate?.role}
        error={deleteError}
        onConfirm={() => deleteCandidate && void handleDeleteConfirm(deleteCandidate.role)}
        onClose={() => {
          setDeleteCandidate(null);
          setDeleteError(null);
        }}
      />

      {/* Create/Edit role modal */}
      <UpsertRoleModal
        key={editingRole?.id ?? (creating ? 'create' : 'closed')}
        open={creating || editingRole !== null}
        editingRole={editingRole}
        availableScopes={availableScopes}
        onClose={() => {
          setCreating(false);
          setEditingRole(null);
        }}
        onSaved={() => {
          setCreating(false);
          setEditingRole(null);
          router.refresh();
        }}
      />
    </Stack>
  );
}

// ── Card grid view ──────────────────────────────────────────────────────────

function RoleCardGrid({
  roles,
  onEdit,
  onDelete,
}: {
  roles: RoleEntry[];
  onEdit?: (role: RoleEntry) => void;
  onDelete?: (role: RoleEntry) => void;
}) {
  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      {roles.map((role) => {
        const isReserved = RESERVED_ROLES.has(role.role);
        const scopes = role.scopes ?? [];
        return (
          <Card key={role.id} column>
            <CardHeader compact>
              <Stack direction="horizontal" align="center" gap="tight">
                <Text size="caption" mono weight="emphasis">
                  {role.role}
                </Text>
                {isReserved && (
                  <Badge variant="secondary" size="sm">
                    reserved
                  </Badge>
                )}
              </Stack>
            </CardHeader>

            <CardContent grow>
              {scopes.length === 0 ? (
                <Text size="caption" color="muted">
                  No scopes assigned
                </Text>
              ) : (
                <ScopeChipList scopes={scopes} max={8} />
              )}
            </CardContent>

            <CardFooter compact justify="between">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="subtle">
                  {scopes.length} {scopes.length === 1 ? 'scope' : 'scopes'}
                </Text>
                {role.budget && (
                  <Text size="caption" color="muted">
                    {fmtUsd(role.budget.limit_usd ?? 0)}
                    {fmtWindowSuffix(role.budget.reset_cadence)}
                    {role.budget.pooled ? ' · pool' : ''}
                  </Text>
                )}
              </Stack>
              {!isReserved && (
                <Stack direction="horizontal" align="center" gap="snug">
                  {role.created_at && (
                    <Text size="caption" color="subtle">
                      Created {fmtDate(role.created_at)}
                    </Text>
                  )}
                  {onEdit && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onEdit(role)}
                      aria-label="Edit role"
                    >
                      <Icon name="edit" size="2xs" />
                    </Button>
                  )}
                  {onDelete && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onDelete(role)}
                      aria-label="Delete role"
                    >
                      <Icon name="trash" size="2xs" />
                    </Button>
                  )}
                </Stack>
              )}
            </CardFooter>
          </Card>
        );
      })}
    </Grid>
  );
}

// ── Table view ──────────────────────────────────────────────────────────────

function RoleTable({
  roles,
  onEdit,
  onDelete,
  viewToggle,
}: {
  roles: RoleEntry[];
  onEdit?: (role: RoleEntry) => void;
  onDelete?: (role: RoleEntry) => void;
  viewToggle: React.ReactNode;
}) {
  const columns = useMemo<DataTableColumn<RoleEntry>[]>(
    () => [
      {
        id: 'role',
        header: 'Role',
        gridWidth: '14rem',
        minWidth: '12rem',
        locked: true,
        sortable: true,
        sortAccessor: (r) => r.role,
        cell: (r) => {
          const isReserved = RESERVED_ROLES.has(r.role);
          return (
            <Stack direction="horizontal" align="center" gap="tight">
              <Text size="caption" mono weight="emphasis">
                {r.role}
              </Text>
              {isReserved && (
                <Badge variant="secondary" size="sm">
                  reserved
                </Badge>
              )}
            </Stack>
          );
        },
      },
      {
        id: 'scopes',
        header: 'Scopes',
        gridWidth: 'minmax(0, 3fr)',
        minWidth: '14rem',
        wrap: true,
        filter: scopeColumnFilter(roles, (r) => r.scopes ?? []),
        cell: (r) => <ScopeChipList scopes={r.scopes ?? []} maxRows={2} />,
      },
      {
        id: 'budget',
        header: 'Budget',
        gridWidth: '8rem',
        minWidth: '6rem',
        cell: (r) =>
          r.budget ? (
            <Text size="caption" color="muted">
              {fmtUsd(r.budget.limit_usd ?? 0)}
              {fmtWindowSuffix(r.budget.reset_cadence)}
              {r.budget.pooled ? ' · pool' : ''}
            </Text>
          ) : (
            <Text size="caption" color="subtle">
              —
            </Text>
          ),
      },
      {
        id: 'created',
        header: 'Created',
        gridWidth: '6rem',
        minWidth: '5.5rem',
        sortable: true,
        sortAccessor: (r) => r.created_at ?? '',
        cell: (r) => {
          const isReserved = RESERVED_ROLES.has(r.role);
          return (
            <Text size="caption" color="subtle">
              {!isReserved && r.created_at ? fmtDate(r.created_at) : '—'}
            </Text>
          );
        },
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '5rem',
        cell: (r) => {
          const isReserved = RESERVED_ROLES.has(r.role);
          if (isReserved || (!onEdit && !onDelete)) return null;
          return (
            <Stack direction="horizontal" gap="tight">
              {onEdit && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    onEdit(r);
                  }}
                  aria-label="Edit role"
                >
                  <Icon name="edit" size="2xs" />
                </Button>
              )}
              {onDelete && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(r);
                  }}
                  aria-label="Delete role"
                >
                  <Icon name="trash" size="2xs" />
                </Button>
              )}
            </Stack>
          );
        },
      },
    ],
    [onEdit, onDelete, roles],
  );

  return (
    <SmartTable
      tableId="policyRoles"
      data={roles}
      columns={columns}
      getRowKey={(r) => r.id}
      noun="roles"
      searchPlaceholder="Search roles…"
      searchFn={(r, q) =>
        r.role.toLowerCase().includes(q) ||
        (r.scopes ?? []).some((s) => s.toLowerCase().includes(q))
      }
      defaultSortId="role"
      defaultSortDir="asc"
      emptyMessage="No roles yet."
      filteredEmptyMessage="No roles match your search."
      toolbarActions={viewToggle}
    />
  );
}

// ── Create / Edit modal ──────────────────────────────────────────────────────

function UpsertRoleModal({
  open,
  editingRole,
  availableScopes,
  onClose,
  onSaved,
}: {
  open: boolean;
  editingRole: RoleEntry | null;
  availableScopes: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  // Initialized from props; component is keyed so it remounts for each open/edit.
  const [roleName, setRoleName] = useState(editingRole?.role ?? '');
  const [selectedScopes, setSelectedScopes] = useState<string[]>([...(editingRole?.scopes ?? [])]);
  const [budgetEnabled, setBudgetEnabled] = useState(!!editingRole?.budget);
  const [limitUSD, setLimitUSD] = useState(
    editingRole?.budget?.limit_usd ? String(editingRole.budget.limit_usd) : '',
  );
  const [cadence, setCadence] = useState(editingRole?.budget?.reset_cadence ?? 'monthly');
  const [pooled, setPooled] = useState(editingRole?.budget?.pooled ?? false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    if (!roleName || selectedScopes.length === 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const budget =
      budgetEnabled && limitUSD
        ? { limit_usd: parseFloat(limitUSD), reset_cadence: cadence, pooled }
        : null;
    const result = await upsertRoleAction({
      role: roleName,
      scopes: selectedScopes,
      budget,
    });
    if (result.ok) {
      onSaved();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [roleName, selectedScopes, budgetEnabled, limitUSD, cadence, pooled, onSaved]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader title={editingRole ? 'Edit Role' : 'Create Role'} />
      <ModalBody flushY constrained>
        <Stack gap="comfortable">
          {/* Role name */}
          <Stack gap="tight">
            <Eyebrow>Role Name</Eyebrow>
            <Input
              type="text"
              placeholder="e.g. ml-engineer"
              value={roleName}
              onChange={(e) => setRoleName(e.target.value)}
              disabled={!!editingRole}
            />
          </Stack>

          {/* Scopes */}
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" gap="tight">
              <Eyebrow>Scopes</Eyebrow>
              <Text size="caption" color="subtle">
                ({selectedScopes.length} selected)
              </Text>
            </Stack>
            <ScopePicker
              scopes={availableScopes}
              selected={selectedScopes}
              onChange={setSelectedScopes}
            />
          </Stack>

          {/* Budget */}
          <Stack gap="snug">
            <ToggleRow
              label="Budget"
              description="Spend ceiling on this role"
              htmlFor="role-budget-toggle"
              control={
                <Toggle
                  id="role-budget-toggle"
                  checked={budgetEnabled}
                  onChange={setBudgetEnabled}
                />
              }
            />
            {budgetEnabled && (
              <Stack gap="snug">
                <Stack direction="horizontal" align="center" gap="snug">
                  <Input
                    type="number"
                    placeholder="e.g. 50"
                    startAdornment="$"
                    value={limitUSD}
                    onChange={(e) => setLimitUSD(e.target.value)}
                  />
                  <ButtonGroup>
                    <Button
                      variant="outline-muted"
                      size="sm"
                      selected={cadence === 'weekly'}
                      onClick={() => setCadence('weekly')}
                    >
                      Weekly
                    </Button>
                    <Button
                      variant="outline-muted"
                      size="sm"
                      selected={cadence === 'monthly'}
                      onClick={() => setCadence('monthly')}
                    >
                      Monthly
                    </Button>
                  </ButtonGroup>
                </Stack>
                <Stack direction="horizontal" align="center" justify="between" gap="snug">
                  <Text size="caption" color="muted">
                    {pooled
                      ? 'Pooled: one shared pot for all members.'
                      : 'Per member: each principal with this role gets this limit.'}
                  </Text>
                  <ButtonGroup>
                    <Button
                      variant="outline-muted"
                      size="sm"
                      selected={!pooled}
                      onClick={() => setPooled(false)}
                    >
                      Per member
                    </Button>
                    <Button
                      variant="outline-muted"
                      size="sm"
                      selected={pooled}
                      onClick={() => setPooled(true)}
                    >
                      Pooled
                    </Button>
                  </ButtonGroup>
                </Stack>
              </Stack>
            )}
          </Stack>

          {submitError && (
            <Text color="status-error" size="caption">
              {submitError}
            </Text>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          size="sm"
          onClick={() => void handleSubmit()}
          disabled={submitting || !roleName || selectedScopes.length === 0}
        >
          {submitting ? 'Saving...' : editingRole ? 'Update Role' : 'Create Role'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
