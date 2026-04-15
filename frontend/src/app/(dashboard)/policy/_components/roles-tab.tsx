'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { LayoutGrid, List, Pencil, Trash2, Plus, X } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Grid,
  Heading,
  Stack,
  Text,
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@orbit/terra';
import type { RoleEntry } from '../_types';
import { upsertRoleAction, deleteRoleAction } from '../_actions/roles';
import { ScopeChip } from './scope-chip';
import { ScopePicker } from './scope-picker';

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
  const [roles, setRoles] = useState(initialRoles);
  const [error] = useState(initialError);
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
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4 mr-1" />
          Create Role
        </Button>
      </Stack>

      {/* Info box */}
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Roles map OIDC role claims to platform scopes. When a human authenticates, Orbit resolves
          the union of scopes from all matching roles.
        </Text>
      </section>

      {error && (
        <Card column>
          <CardContent>
            <Text color="status-error">{error}</Text>
          </CardContent>
        </Card>
      )}

      {/* Search + view toggle */}
      <Stack direction="horizontal" align="center" justify="between">
        <span className="relative max-w-xs flex-1">
          <input
            type="text"
            placeholder="Search roles..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
        </span>
        <Stack direction="horizontal" gap="none">
          <span className="[&>button]:rounded-r-none">
            <Button
              variant={viewMode === 'cards' ? 'default' : 'outline'}
              size="icon-sm"
              onClick={() => setViewMode('cards')}
              aria-label="Card view"
            >
              <LayoutGrid className="size-3.5" />
            </Button>
          </span>
          <span className="[&>button]:rounded-l-none -ml-px">
            <Button
              variant={viewMode === 'list' ? 'default' : 'outline'}
              size="icon-sm"
              onClick={() => setViewMode('list')}
              aria-label="List view"
            >
              <List className="size-3.5" />
            </Button>
          </span>
        </Stack>
      </Stack>

      {/* Content */}
      {filtered.length === 0 ? (
        <Card column>
          <CardContent>
            <Text color="muted" size="caption">
              {search ? 'No roles match your search.' : 'No roles configured.'}
            </Text>
          </CardContent>
        </Card>
      ) : viewMode === 'cards' ? (
        <RoleCardGrid roles={filtered} onEdit={setEditingRole} onDelete={setDeleteCandidate} />
      ) : (
        <RoleTable roles={filtered} onEdit={setEditingRole} onDelete={setDeleteCandidate} />
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
  onEdit: (role: RoleEntry) => void;
  onDelete: (role: RoleEntry) => void;
}) {
  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      {roles.map((role) => {
        const isReserved = RESERVED_ROLES.has(role.role);
        const scopes = role.scopes ?? [];
        return (
          <section
            key={role.id}
            className={`rounded-lg border bg-surface overflow-hidden transition-colors ${
              isReserved
                ? 'border-accent/25 hover:border-accent/40'
                : 'border-border-subtle hover:border-border'
            }`}
          >
            {/* Header */}
            <header className="px-5 pt-4 pb-3">
              <Stack direction="horizontal" align="center" gap="tight">
                <span className="font-semibold text-sm font-mono text-foreground">{role.role}</span>
                {isReserved && (
                  <Badge variant="secondary" size="sm">
                    reserved
                  </Badge>
                )}
              </Stack>
            </header>

            {/* Scopes */}
            <section className="px-5 pb-3">
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
            </section>

            {/* Footer */}
            <footer className="px-5 py-2.5 border-t border-border-subtle flex items-center gap-2">
              <span className="text-xs text-subtle">
                {scopes.length} {scopes.length === 1 ? 'scope' : 'scopes'}
              </span>
              {!isReserved && role.created_at && (
                <span className="text-xs text-subtle ml-auto">
                  Created {formatDate(role.created_at)}
                </span>
              )}
              {!isReserved && (
                <>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => onEdit(role)}
                    aria-label="Edit role"
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => onDelete(role)}
                    aria-label="Delete role"
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </>
              )}
            </footer>
          </section>
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
}: {
  roles: RoleEntry[];
  onEdit: (role: RoleEntry) => void;
  onDelete: (role: RoleEntry) => void;
}) {
  return (
    <Card column>
      <CardContent flush>
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Role</TableHead>
                <TableHead>Scopes</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {roles.map((role) => {
                const isReserved = RESERVED_ROLES.has(role.role);
                const scopes = role.scopes ?? [];
                return (
                  <TableRow key={role.id}>
                    <TableCell>
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
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" gap="tight" wrap>
                        {scopes.map((scope) => (
                          <ScopeChip key={scope} scope={scope} />
                        ))}
                      </Stack>
                    </TableCell>
                    <TableCell>
                      {!isReserved && role.created_at ? (
                        <Text size="caption" color="subtle">
                          {formatDate(role.created_at)}
                        </Text>
                      ) : (
                        <Text size="caption" color="subtle">
                          —
                        </Text>
                      )}
                    </TableCell>
                    <TableCell>
                      {!isReserved && (
                        <Stack direction="horizontal" gap="tight">
                          <Button variant="ghost" size="icon-sm" onClick={() => onEdit(role)}>
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button variant="ghost" size="icon-sm" onClick={() => onDelete(role)}>
                            <Trash2 className="size-3.5" />
                          </Button>
                        </Stack>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </TableRoot>
      </CardContent>
    </Card>
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
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (role) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [role]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  return (
    <dialog
      ref={dialogRef}
      className="m-auto w-full max-w-md rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      <section className="p-6">
        <Stack gap="comfortable">
          <Stack direction="horizontal" align="center" justify="between">
            <Heading variant="section">Delete Role</Heading>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </Stack>
          {role && (
            <Text size="caption" color="muted">
              Remove the role <strong>{role.role}</strong> and all its scope mappings? This action
              cannot be undone.
            </Text>
          )}
          {error && (
            <Text color="status-error" size="caption">
              {error}
            </Text>
          )}
          <Stack direction="horizontal" gap="snug" justify="end">
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
          </Stack>
        </Stack>
      </section>
    </dialog>
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
  const dialogRef = useRef<HTMLDialogElement>(null);
  // Initialized from props; component is keyed so it remounts for each open/edit.
  const [roleName, setRoleName] = useState(editingRole?.role ?? '');
  const [selectedScopes, setSelectedScopes] = useState<string[]>([...(editingRole?.scopes ?? [])]);
  const [scopeSearch, setScopeSearch] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [open]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

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
    <dialog
      ref={dialogRef}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      <section className="p-6">
        <Stack gap="comfortable">
          <Stack direction="horizontal" align="center" justify="between">
            <Heading variant="section">{editingRole ? 'Edit Role' : 'Create Role'}</Heading>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </Stack>

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

          <Stack direction="horizontal" gap="snug" justify="end">
            <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={handleSubmit}
              disabled={submitting || !roleName || selectedScopes.length === 0}
            >
              {submitting ? 'Saving...' : editingRole ? 'Update Role' : 'Create Role'}
            </Button>
          </Stack>
        </Stack>
      </section>
    </dialog>
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
