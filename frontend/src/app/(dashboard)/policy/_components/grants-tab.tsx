'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Trash2, Plus, X } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
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
import type { Grant, PrincipalType } from '../_types';
import { createGrantAction, deleteGrantAction } from '../_actions/grants';
import { ScopePicker } from './scope-picker';

// ── Props ────────────────────────────────────────────────────────────────────

interface GrantsTabProps {
  initialGrants: Grant[];
  availableScopes: string[];
  initialError: string | null;
}

// ── Component ────────────────────────────────────────────────────────────────

export function GrantsTab({ initialGrants, availableScopes, initialError }: GrantsTabProps) {
  const router = useRouter();
  const [grants, setGrants] = useState(initialGrants);
  const [error] = useState(initialError);
  const [search, setSearch] = useState('');
  const [creating, setCreating] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<Grant | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Sync with server on navigation
  useEffect(() => {
    setGrants(initialGrants);
  }, [initialGrants]);

  const filtered = useMemo(() => {
    if (!search) return grants;
    const q = search.toLowerCase();
    return grants.filter(
      (g) =>
        g.principal_id.toLowerCase().includes(q) ||
        g.principal_type.toLowerCase().includes(q) ||
        g.scopes.some((s) => s.toLowerCase().includes(q)),
    );
  }, [grants, search]);

  const handleDeleteConfirm = useCallback(
    async (id: string) => {
      setDeletingId(id);
      setDeleteError(null);
      const result = await deleteGrantAction(id);
      if (result.ok) {
        setDeleteCandidate(null);
        setDeletingId(null);
        router.refresh();
      } else {
        setDeleteError(result.message);
        setDeletingId(null);
      }
    },
    [router],
  );

  return (
    <Stack gap="comfortable">
      {/* Header */}
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="none">
          <Heading variant="title">Grants</Heading>
          <Text size="supporting" color="muted">
            Permission grants assigned to principals in your tenant.
          </Text>
        </Stack>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="size-4 mr-1" />
          Create Grant
        </Button>
      </Stack>

      {error && (
        <Card column>
          <CardContent>
            <Text color="status-error">{error}</Text>
          </CardContent>
        </Card>
      )}

      {/* Search */}
      <span className="relative max-w-xs">
        <input
          type="text"
          placeholder="Search grants..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full rounded-md border border-border bg-surface px-3 py-1.5 pl-8 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
      </span>

      {/* Table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Principal</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5}>
                      <Text color="muted" size="caption">
                        {search ? 'No grants match your search.' : 'No grants configured.'}
                      </Text>
                    </TableCell>
                  </TableRow>
                ) : (
                  filtered.map((grant) => (
                    <TableRow key={grant.id}>
                      <TableCell>
                        <Text size="caption" mono>
                          {grant.principal_id}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <PrincipalBadge type={grant.principal_type} />
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" gap="tight" style={{ flexWrap: 'wrap' }}>
                          {grant.scopes.map((scope) => (
                            <Badge key={scope} variant="outline" size="sm">
                              {scope}
                            </Badge>
                          ))}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Text size="caption" color="subtle">
                          {formatDate(grant.created_at)}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Button variant="ghost" size="sm" onClick={() => setDeleteCandidate(grant)}>
                          <Trash2 className="size-3.5" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>

      {/* Delete confirmation modal */}
      <DeleteGrantModal
        grant={deleteCandidate}
        deleting={deletingId === deleteCandidate?.id}
        error={deleteError}
        onConfirm={handleDeleteConfirm}
        onClose={() => {
          setDeleteCandidate(null);
          setDeleteError(null);
        }}
      />

      {/* Create grant modal */}
      <CreateGrantModal
        open={creating}
        availableScopes={availableScopes}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false);
          router.refresh();
        }}
      />
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

// ── Delete modal ─────────────────────────────────────────────────────────────

function DeleteGrantModal({
  grant,
  deleting,
  error,
  onConfirm,
  onClose,
}: {
  grant: Grant | null;
  deleting: boolean;
  error: string | null;
  onConfirm: (id: string) => void;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (grant) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [grant]);

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
            <Heading variant="section">Delete Grant</Heading>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </Stack>
          {grant && (
            <Text size="caption" color="muted">
              Remove the grant for <strong>{grant.principal_id}</strong> ({grant.principal_type})?
              This action cannot be undone.
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
              onClick={() => grant && onConfirm(grant.id)}
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

// ── Create modal ─────────────────────────────────────────────────────────────

function CreateGrantModal({
  open,
  availableScopes,
  onClose,
  onCreated,
}: {
  open: boolean;
  availableScopes: string[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [principalType, setPrincipalType] = useState<PrincipalType>('AGENT');
  const [principalId, setPrincipalId] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
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
    if (!principalId || selectedScopes.length === 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const result = await createGrantAction({
      principal_type: principalType,
      principal_id: principalId,
      scopes: selectedScopes,
    });
    if (result.ok) {
      setPrincipalId('');
      setSelectedScopes([]);
      setScopeSearch('');
      onCreated();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [principalType, principalId, selectedScopes, onCreated]);

  return (
    <dialog
      ref={dialogRef}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      <section className="p-6">
        <Stack gap="comfortable">
          <Stack direction="horizontal" align="center" justify="between">
            <Heading variant="section">Create Grant</Heading>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </Stack>

          {/* Principal type */}
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Principal Type
            </Text>
            <Stack direction="horizontal" gap="tight">
              {(['AGENT', 'HUMAN', 'SERVICE'] as PrincipalType[]).map((t) => (
                <Button
                  key={t}
                  variant={principalType === t ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setPrincipalType(t)}
                >
                  {t}
                </Button>
              ))}
            </Stack>
          </Stack>

          {/* Principal ID */}
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Principal ID
            </Text>
            <input
              type="text"
              placeholder="e.g. agent-uuid or user@example.com"
              value={principalId}
              onChange={(e) => setPrincipalId(e.target.value)}
              className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
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
              disabled={submitting || !principalId || selectedScopes.length === 0}
            >
              {submitting ? 'Creating...' : 'Create Grant'}
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
