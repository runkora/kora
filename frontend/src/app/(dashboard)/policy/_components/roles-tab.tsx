'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  DataTable,
  Grid,
  Heading,
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
import type { RoleEntry } from '../_types';
import { upsertRoleAction, deleteRoleAction } from '../_actions/roles';
import { usePermissions } from '../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { ScopeChip } from './scope-chip';
import { ScopePicker } from './scope-picker';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

// ── Reserved roles (cannot be modified via UI) ───────────────────────────────

const RESERVED_ROLES = new Set(['tenant-admin', 'platform-admin']);

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
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="none">
          <Heading variant="title">Roles</Heading>
          <Text size="supporting" color="muted">
            Map OIDC role names to platform scopes.
          </Text>
        </Stack>
        {canWrite && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Icon name="plus" />
            Create Role
          </Button>
        )}
      </Stack>

      {/* Info box */}
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Roles map OIDC role claims to platform scopes. When a human authenticates, Orbit resolves
          the union of scopes from all matching roles.
        </Text>
      </section>

      {error && <ErrorBanner message={error} />}

      {/* Search + view toggle — card mode only; list mode toolbar is owned by DataTable */}
      {viewMode === 'cards' && (
        <Stack direction="horizontal" align="center" justify="between">
          <Input
            type="text"
            placeholder="Search roles..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="max-w-xs flex-1"
          />
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
      <DeleteRoleModal
        role={deleteCandidate}
        deleting={deletingRole === deleteCandidate?.role}
        error={deleteError}
        onConfirm={handleDeleteConfirm}
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
              <Stack direction="horizontal" gap="tight" wrap>
                {scopes.map((scope) => (
                  <ScopeChip key={scope} scope={scope} />
                ))}
                {scopes.length === 0 && (
                  <Text size="caption" color="muted">
                    No scopes assigned
                  </Text>
                )}
              </Stack>
            </CardContent>

            <CardFooter compact justify="between">
              <Text size="caption" color="subtle">
                {scopes.length} {scopes.length === 1 ? 'scope' : 'scopes'}
              </Text>
              {!isReserved && (
                <Stack direction="horizontal" align="center" gap="snug">
                  {role.created_at && (
                    <Text size="caption" color="subtle">
                      Created {formatDate(role.created_at)}
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

// ── View toggle ──────────────────────────────────────────────────────────────

function ViewToggle({
  viewMode,
  onToggle,
}: {
  viewMode: 'cards' | 'list';
  onToggle: (mode: 'cards' | 'list') => void;
}) {
  return (
    <Stack direction="horizontal" gap="none">
      {/* eslint-disable-next-line local/no-raw-div */}
      <div className="[&>button]:rounded-r-none">
        <Button
          variant={viewMode === 'cards' ? 'default' : 'outline'}
          size="icon-sm"
          onClick={() => onToggle('cards')}
          aria-label="Card view"
        >
          <Icon name="layout-grid" size="2xs" />
        </Button>
      </div>
      {/* eslint-disable-next-line local/no-raw-div */}
      <div className="[&>button]:rounded-l-none -ml-px">
        <Button
          variant={viewMode === 'list' ? 'default' : 'outline'}
          size="icon-sm"
          onClick={() => onToggle('list')}
          aria-label="List view"
        >
          <Icon name="list" size="2xs" />
        </Button>
      </div>
    </Stack>
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
        cell: (r) => (
          <Stack direction="horizontal" gap="tight" wrap>
            {(r.scopes ?? []).map((scope) => (
              <ScopeChip key={scope} scope={scope} />
            ))}
          </Stack>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        sortable: true,
        sortAccessor: (r) => r.created_at ?? '',
        cell: (r) => {
          const isReserved = RESERVED_ROLES.has(r.role);
          return (
            <Text size="caption" color="subtle">
              {!isReserved && r.created_at ? formatDate(r.created_at) : '—'}
            </Text>
          );
        },
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '5rem',
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
    [onEdit, onDelete],
  );

  return (
    <DataTable
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

// ── Delete modal ─────────────────────────────────────────────────────────────

function DeleteRoleModal({
  role,
  deleting,
  error,
  onConfirm,
  onClose,
}: {
  role: RoleEntry | null;
  deleting: boolean;
  error: string | null;
  onConfirm: (role: string) => void;
  onClose: () => void;
}) {
  return (
    <Modal open={!!role} onClose={onClose} size="sm">
      <ModalHeader title="Delete Role" />
      <ModalBody>
        <Stack gap="comfortable">
          {role && (
            <Text size="caption" color="muted">
              Remove the role &ldquo;{role.role}&rdquo; and all its scope mappings? This action
              cannot be undone.
            </Text>
          )}
          {error && (
            <Text color="status-error" size="caption">
              {error}
            </Text>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={deleting}>
          Cancel
        </Button>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => role && onConfirm(role.role)}
          disabled={deleting}
        >
          {deleting ? 'Deleting...' : 'Delete'}
        </Button>
      </ModalFooter>
    </Modal>
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
  const [scopeSearch, setScopeSearch] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    if (!roleName || selectedScopes.length === 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const result = await upsertRoleAction({
      role: roleName,
      scopes: selectedScopes,
    });
    if (result.ok) {
      onSaved();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [roleName, selectedScopes, onSaved]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader title={editingRole ? 'Edit Role' : 'Create Role'} />
      <ModalBody>
        <Stack gap="comfortable">
          {/* Role name */}
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Role Name
            </Text>
            <input
              type="text"
              placeholder="e.g. ml-engineer"
              value={roleName}
              onChange={(e) => setRoleName(e.target.value)}
              disabled={!!editingRole}
              className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-50"
            />
          </Stack>

          {/* Scopes */}
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Scopes ({selectedScopes.length} selected)
            </Text>
            <ScopePicker
              availableScopes={availableScopes}
              selectedScopes={selectedScopes}
              onToggle={(scope, checked) => {
                if (checked) {
                  setSelectedScopes((prev) => [...prev, scope]);
                } else {
                  setSelectedScopes((prev) => prev.filter((s) => s !== scope));
                }
              }}
              onToggleGroup={(scopes, checked) => {
                if (checked) {
                  setSelectedScopes((prev) => [...new Set([...prev, ...scopes])]);
                } else {
                  const remove = new Set(scopes);
                  setSelectedScopes((prev) => prev.filter((s) => !remove.has(s)));
                }
              }}
              search={scopeSearch}
              onSearchChange={setScopeSearch}
            />
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

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    });
  } catch {
    return iso;
  }
}
