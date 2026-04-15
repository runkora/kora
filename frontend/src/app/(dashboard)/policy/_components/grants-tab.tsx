'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { LayoutGrid, List, Search, ShieldCheck, Trash2, Plus, X } from 'lucide-react';
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
import type { Grant, PrincipalType } from '../_types';
import { createGrantAction, deleteGrantAction } from '../_actions/grants';
import { ScopeChip } from './scope-chip';
import { ScopePicker } from './scope-picker';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

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
  const [error, setError] = useState(initialError);

  // Sync error state when parent re-renders with new error
  useEffect(() => {
    setError(initialError);
  }, [initialError]);
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState<PrincipalType | 'all'>('all');
  const [viewMode, setViewMode] = useState<'cards' | 'list'>('cards');
  const [creating, setCreating] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<Grant | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Sync with server on navigation
  useEffect(() => {
    setGrants(initialGrants);
  }, [initialGrants]);

  const typeCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const g of grants) {
      counts[g.principal_type] = (counts[g.principal_type] ?? 0) + 1;
    }
    return counts;
  }, [grants]);

  const filtered = useMemo(() => {
    let result = grants;
    if (typeFilter !== 'all') {
      result = result.filter((g) => g.principal_type === typeFilter);
    }
    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (g) =>
          g.principal_id.toLowerCase().includes(q) ||
          g.principal_type.toLowerCase().includes(q) ||
          g.scopes.some((s) => s.toLowerCase().includes(q)),
      );
    }
    return result;
  }, [grants, search, typeFilter]);

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

      {/* Info box */}
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Grants assign specific scopes directly to a principal. Unlike roles (which map OIDC claims
          to scopes), grants are explicit — they apply regardless of how the principal
          authenticates.
        </Text>
      </section>

      {error && <ErrorBanner message={error} />}

      {/* Search + view toggle */}
      <Stack direction="horizontal" align="center" justify="between">
        <span className="relative max-w-xs flex-1">
          <input
            type="text"
            placeholder="Search grants..."
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

      {/* Filter chips */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <FilterChip
          label="All"
          count={grants.length}
          active={typeFilter === 'all'}
          onClick={() => setTypeFilter('all')}
        />
        {(['AGENT', 'HUMAN', 'SERVICE'] as PrincipalType[]).map((type) => (
          <FilterChip
            key={type}
            label={type}
            count={typeCounts[type] ?? 0}
            active={typeFilter === type}
            onClick={() => setTypeFilter(type)}
          />
        ))}
        <span className="ml-auto">
          <Text size="caption" color="subtle">
            {filtered.length} of {grants.length} grants
          </Text>
        </span>
      </Stack>

      {/* Content */}
      {filtered.length === 0 ? (
        search || typeFilter !== 'all' ? (
          <EmptyState
            icon={<Search className="size-5" />}
            title="No matches"
            description="No grants match your current filters. Try adjusting your search or clearing the filter."
          />
        ) : (
          <EmptyState
            icon={<ShieldCheck className="size-5" />}
            title="No grants yet"
            description="Grants assign scopes directly to principals. Create your first grant to give an agent, human, or service access to specific operations."
            action={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus className="size-4 mr-1" />
                Create Grant
              </Button>
            }
          />
        )
      ) : viewMode === 'cards' ? (
        <GrantCardGrid grants={filtered} onDelete={setDeleteCandidate} />
      ) : (
        <GrantTable grants={filtered} onDelete={setDeleteCandidate} />
      )}

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
        key={creating ? 'create' : 'closed'}
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

// ── Filter Chip ─────────────────────────────────────────────────────────────

function FilterChip({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border transition-colors select-none cursor-pointer',
        active
          ? 'bg-accent/10 text-accent border-accent/30'
          : 'bg-transparent text-muted border-border hover:bg-surface-elevated hover:text-foreground',
      ].join(' ')}
    >
      <span>{label}</span>
      <span className="text-[0.6875rem] opacity-70">{count}</span>
    </button>
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

// ── Card grid view ──────────────────────────────────────────────────────────

function GrantCardGrid({
  grants,
  onDelete,
}: {
  grants: Grant[];
  onDelete: (grant: Grant) => void;
}) {
  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      {grants.map((grant) => (
        <section
          key={grant.id}
          className="rounded-lg border border-border-subtle bg-surface overflow-hidden transition-colors hover:border-border"
        >
          {/* Header */}
          <header className="px-5 pt-4 pb-3">
            <Stack direction="horizontal" align="center" gap="snug">
              <PrincipalBadge type={grant.principal_type} />
              <Text size="caption" mono weight="emphasis">
                {grant.principal_id}
              </Text>
            </Stack>
          </header>

          {/* Scopes */}
          <section className="px-5 pb-3">
            <Stack direction="horizontal" gap="tight" wrap>
              {grant.scopes.map((scope) => (
                <ScopeChip key={scope} scope={scope} />
              ))}
            </Stack>
          </section>

          {/* Footer */}
          <footer className="px-5 py-2.5 border-t border-border-subtle flex items-center gap-2">
            <span className="text-xs text-subtle">
              {grant.scopes.length} {grant.scopes.length === 1 ? 'scope' : 'scopes'}
            </span>
            <span className="text-xs text-subtle ml-auto">
              Created {formatDate(grant.created_at)}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onDelete(grant)}
              aria-label="Delete grant"
            >
              <Trash2 className="size-3.5" />
            </Button>
          </footer>
        </section>
      ))}
    </Grid>
  );
}

// ── Table view ──────────────────────────────────────────────────────────────

function GrantTable({ grants, onDelete }: { grants: Grant[]; onDelete: (grant: Grant) => void }) {
  return (
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
              {grants.map((grant) => (
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
                    <Stack direction="horizontal" gap="tight" wrap>
                      {grant.scopes.map((scope) => (
                        <ScopeChip key={scope} scope={scope} />
                      ))}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Text size="caption" color="subtle">
                      {formatDate(grant.created_at)}
                    </Text>
                  </TableCell>
                  <TableCell>
                    <Button variant="ghost" size="icon-sm" onClick={() => onDelete(grant)}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableRoot>
      </CardContent>
    </Card>
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
            <Stack gap="snug">
              <Text size="caption" color="muted">
                Remove this grant? This action cannot be undone.
              </Text>
              <section className="rounded-md border border-border-subtle bg-surface-elevated px-4 py-3">
                <Stack gap="snug">
                  <Stack direction="horizontal" align="center" gap="snug">
                    <PrincipalBadge type={grant.principal_type} />
                    <Text size="caption" mono weight="emphasis">
                      {grant.principal_id}
                    </Text>
                  </Stack>
                  <Stack direction="horizontal" gap="tight" wrap>
                    {grant.scopes.map((s) => (
                      <ScopeChip key={s} scope={s} />
                    ))}
                  </Stack>
                </Stack>
              </section>
            </Stack>
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
    const trimmedId = principalId.trim();
    if (!trimmedId || selectedScopes.length === 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const result = await createGrantAction({
      principal_type: principalType,
      principal_id: trimmedId,
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
