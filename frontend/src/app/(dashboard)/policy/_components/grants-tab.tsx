'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  ButtonGroup,
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
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import type { Grant, PrincipalType } from '../_types';
import { createGrantAction, deleteGrantAction, searchUsersAction } from '../_actions/grants';
import { usePermissions } from '../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { ScopeChip } from './scope-chip';
import { ScopePicker } from './scope-picker';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

// ── Props ────────────────────────────────────────────────────────────────────

interface GrantsTabProps {
  initialGrants: Grant[];
  availableScopes: string[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  initialError: string | null;
  page: number;
  limit: number;
  total: number;
}

// ── Component ────────────────────────────────────────────────────────────────

export function GrantsTab({
  initialGrants,
  availableScopes,
  patAttribution,
  userAttribution,
  initialError,
  page,
  limit,
  total,
}: GrantsTabProps) {
  const router = useRouter();
  const { scopes: sessionScopes } = usePermissions();
  const canWrite = hasScope(sessionScopes, 'platform:grants:write');
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
      result = result.filter((g) => {
        const identity = resolveGrantIdentity(g, patAttribution, userAttribution);
        return (
          identity.primary.toLowerCase().includes(q) ||
          (identity.email?.toLowerCase().includes(q) ?? false) ||
          (identity.ownerName?.toLowerCase().includes(q) ?? false) ||
          (identity.ownerEmail?.toLowerCase().includes(q) ?? false) ||
          g.principal_id.toLowerCase().includes(q) ||
          g.principal_type.toLowerCase().includes(q) ||
          g.scopes.some((s) => s.toLowerCase().includes(q))
        );
      });
    }
    return result;
  }, [grants, search, typeFilter, patAttribution, userAttribution]);

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
        {canWrite && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Icon name="plus" />
            Create Grant
          </Button>
        )}
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

      {/* Search + filter chips + view toggle — card mode only; list mode toolbar is owned by DataTable */}
      {viewMode === 'cards' && (
        <>
          <Stack direction="horizontal" align="center" justify="between">
            <Stack asChild grow maxWidth="compact">
              <Input
                placeholder="Search grants..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </Stack>
            <GrantViewToggle viewMode={viewMode} onToggle={setViewMode} />
          </Stack>

          <Stack direction="horizontal" align="center" justify="between">
            <Stack direction="horizontal" align="center" gap="snug" wrap>
              <FilterChip
                label="All"
                count={grants.length}
                active={typeFilter === 'all'}
                onClick={() => setTypeFilter('all')}
              />
              {(['AGENT', 'HUMAN', 'PAT', 'SERVICE'] as PrincipalType[]).map((type) => (
                <FilterChip
                  key={type}
                  label={type}
                  count={typeCounts[type] ?? 0}
                  active={typeFilter === type}
                  onClick={() => setTypeFilter(type)}
                />
              ))}
            </Stack>
            <Text size="caption" color="subtle">
              {filtered.length} shown · {total} total
            </Text>
          </Stack>
        </>
      )}

      {/* Content */}
      {viewMode === 'cards' ? (
        filtered.length === 0 ? (
          search || typeFilter !== 'all' ? (
            <EmptyState
              icon="search"
              title="No matches"
              description="No grants match your current filters. Try adjusting your search or clearing the filter."
            />
          ) : (
            <EmptyState
              icon="shield-check"
              title="No grants yet"
              description="Grants assign scopes directly to principals. Create your first grant to give an agent, human, or service access to specific operations."
              action={
                canWrite ? (
                  <Button size="sm" onClick={() => setCreating(true)}>
                    <Icon name="plus" />
                    Create Grant
                  </Button>
                ) : undefined
              }
            />
          )
        ) : (
          <GrantCardGrid
            grants={filtered}
            patAttribution={patAttribution}
            userAttribution={userAttribution}
            onDelete={canWrite ? setDeleteCandidate : undefined}
          />
        )
      ) : (
        <GrantTable
          grants={grants}
          patAttribution={patAttribution}
          userAttribution={userAttribution}
          onDelete={canWrite ? setDeleteCandidate : undefined}
          viewToggle={<GrantViewToggle viewMode={viewMode} onToggle={setViewMode} />}
        />
      )}

      {/* Pagination */}
      {total > limit && <GrantsPagination page={page} limit={limit} total={total} />}

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

// ── Pagination ───────────────────────────────────────────────────────────────

function GrantsPagination({ page, limit, total }: { page: number; limit: number; total: number }) {
  const router = useRouter();
  const totalPages = Math.ceil(total / limit);
  const start = (page - 1) * limit + 1;
  const end = Math.min(page * limit, total);

  return (
    <Stack direction="horizontal" align="center" justify="between">
      <Text size="caption" color="subtle">
        {start}–{end} of {total} grants
      </Text>
      <Stack direction="horizontal" gap="snug">
        <Button
          variant="outline"
          size="sm"
          disabled={page <= 1}
          onClick={() => router.push(`?page=${page - 1}`)}
        >
          Previous
        </Button>
        <div className="flex items-center px-2">
          <Text size="caption" color="muted">
            Page {page} of {totalPages}
          </Text>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => router.push(`?page=${page + 1}`)}
        >
          Next
        </Button>
      </Stack>
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
      {label}
      <Text asChild size="micro" color="muted">
        <output>{count}</output>
      </Text>
    </button>
  );
}

// ── PrincipalBadge ───────────────────────────────────────────────────────────

function PrincipalBadge({ type }: { type: PrincipalType }) {
  const variant =
    type === 'AGENT'
      ? 'default'
      : type === 'HUMAN'
        ? 'healthy'
        : type === 'PAT'
          ? 'secondary'
          : 'degraded';
  return (
    <Badge variant={variant} size="sm">
      {type}
    </Badge>
  );
}

// ── Card grid view ──────────────────────────────────────────────────────────

interface GrantIdentity {
  primary: string; // display name for HUMAN, UUID for others
  email?: string; // email for HUMAN
  ownerName?: string; // PAT owner display name
  ownerEmail?: string; // PAT owner email
  rawId?: string; // principal_id, shown when primary is a resolved name
}

function resolveGrantIdentity(
  grant: Grant,
  patAttribution: Record<string, PATGrantAttribution>,
  userAttribution: Record<string, PATAttributionEntry>,
): GrantIdentity {
  if (grant.principal_type === 'HUMAN') {
    const u = userAttribution[grant.principal_id];
    if (u) {
      return {
        primary: u.display_name || u.email,
        email: u.display_name ? u.email : undefined,
        rawId: grant.principal_id,
      };
    }
    return { primary: grant.principal_id };
  }
  if (grant.principal_type === 'PAT') {
    const pat = patAttribution[grant.principal_id];
    const owner = pat?.owner;
    return {
      primary: pat?.pat_name ?? grant.principal_id,
      ownerName: owner ? owner.display_name || owner.email : undefined,
      ownerEmail: owner?.display_name ? owner.email : undefined,
      rawId: grant.principal_id,
    };
  }
  return { primary: grant.principal_id };
}

function GrantCardGrid({
  grants,
  patAttribution,
  userAttribution,
  onDelete,
}: {
  grants: Grant[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  onDelete?: (grant: Grant) => void;
}) {
  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      {grants.map((grant) => {
        const identity = resolveGrantIdentity(grant, patAttribution, userAttribution);
        return (
          <Card key={grant.id} column>
            <CardHeader compact>
              <Stack gap="snug">
                <Stack gap="tight">
                  <Stack direction="horizontal" align="center" gap="snug">
                    <PrincipalBadge type={grant.principal_type} />
                    <Text size="caption" mono={grant.principal_type !== 'HUMAN'} weight="emphasis">
                      {identity.primary}
                    </Text>
                  </Stack>
                  {identity.email && (
                    <Text size="caption" color="muted">
                      {identity.email}
                    </Text>
                  )}
                  {identity.ownerName && (
                    <Text size="caption" color="muted">
                      {identity.ownerEmail
                        ? `${identity.ownerName} · ${identity.ownerEmail}`
                        : identity.ownerName}
                    </Text>
                  )}
                </Stack>
                {identity.rawId && (
                  <Text size="caption" color="subtle" mono>
                    {identity.rawId}
                  </Text>
                )}
              </Stack>
            </CardHeader>

            <CardContent grow>
              <Stack direction="horizontal" gap="tight" wrap>
                {grant.scopes.map((scope) => (
                  <ScopeChip key={scope} scope={scope} />
                ))}
              </Stack>
            </CardContent>

            <CardFooter compact justify="between">
              <Text size="caption" color="subtle">
                {grant.scopes.length} {grant.scopes.length === 1 ? 'scope' : 'scopes'}
              </Text>
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="subtle">
                  Created {formatDate(grant.created_at)}
                </Text>
                {onDelete && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => onDelete(grant)}
                    aria-label="Delete grant"
                  >
                    <Icon name="trash" size="2xs" />
                  </Button>
                )}
              </Stack>
            </CardFooter>
          </Card>
        );
      })}
    </Grid>
  );
}

// ── View toggle ──────────────────────────────────────────────────────────────

function GrantViewToggle({
  viewMode,
  onToggle,
}: {
  viewMode: 'cards' | 'list';
  onToggle: (mode: 'cards' | 'list') => void;
}) {
  return (
    <ButtonGroup>
      <Button
        variant={viewMode === 'cards' ? 'default' : 'outline'}
        size="icon-sm"
        onClick={() => onToggle('cards')}
        aria-label="Card view"
      >
        <Icon name="layout-grid" size="2xs" />
      </Button>
      <Button
        variant={viewMode === 'list' ? 'default' : 'outline'}
        size="icon-sm"
        onClick={() => onToggle('list')}
        aria-label="List view"
      >
        <Icon name="list" size="2xs" />
      </Button>
    </ButtonGroup>
  );
}

// ── Table view ──────────────────────────────────────────────────────────────

const TYPE_OPTIONS = [
  { value: 'AGENT', label: 'Agent' },
  { value: 'HUMAN', label: 'Human' },
  { value: 'PAT', label: 'PAT' },
  { value: 'SERVICE', label: 'Service' },
];

function GrantTable({
  grants,
  patAttribution,
  userAttribution,
  onDelete,
  viewToggle,
}: {
  grants: Grant[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  onDelete?: (grant: Grant) => void;
  viewToggle: React.ReactNode;
}) {
  const columns = useMemo<DataTableColumn<Grant>[]>(
    () => [
      {
        id: 'principal',
        header: 'Principal',
        locked: true,
        sortable: true,
        sortAccessor: (g) => resolveGrantIdentity(g, patAttribution, userAttribution).primary,
        cell: (g) => {
          const identity = resolveGrantIdentity(g, patAttribution, userAttribution);
          return (
            <Stack gap="none">
              <Text size="caption" mono={g.principal_type !== 'HUMAN'} weight="emphasis">
                {identity.primary}
              </Text>
              {identity.email && (
                <Text size="caption" color="muted">
                  {identity.email}
                </Text>
              )}
              {identity.ownerName && (
                <Text size="caption" color="muted">
                  {identity.ownerEmail
                    ? `${identity.ownerName} · ${identity.ownerEmail}`
                    : identity.ownerName}
                </Text>
              )}
            </Stack>
          );
        },
      },
      {
        id: 'id',
        header: 'ID',
        cell: (g) => {
          const { rawId, primary } = resolveGrantIdentity(g, patAttribution, userAttribution);
          const id = rawId ?? primary;
          return (
            <div className="whitespace-nowrap">
              <Text size="caption" color="subtle" mono>
                {id}
              </Text>
            </div>
          );
        },
      },
      {
        id: 'type',
        header: 'Type',
        sortable: true,
        sortAccessor: (g) => g.principal_type,
        filter: {
          mode: 'client',
          options: TYPE_OPTIONS,
          filterFn: (g, selected) => selected.has(g.principal_type),
        },
        cell: (g) => <PrincipalBadge type={g.principal_type} />,
      },
      {
        id: 'scopes',
        header: 'Scopes',
        cell: (g) => (
          <Stack direction="horizontal" gap="tight" wrap>
            {g.scopes.map((scope) => (
              <ScopeChip key={scope} scope={scope} />
            ))}
          </Stack>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        sortable: true,
        sortAccessor: (g) => g.created_at,
        cell: (g) => (
          <div className="whitespace-nowrap">
            <Text size="caption" color="subtle">
              {formatDate(g.created_at)}
            </Text>
          </div>
        ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '3rem',
        cell: (g) =>
          onDelete ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={(e) => {
                e.stopPropagation();
                onDelete(g);
              }}
              aria-label="Delete grant"
            >
              <Icon name="trash" size="2xs" />
            </Button>
          ) : null,
      },
    ],
    [onDelete, patAttribution, userAttribution],
  );

  return (
    <DataTable
      data={grants}
      columns={columns}
      getRowKey={(g) => g.id}
      noun="grants"
      searchPlaceholder="Search grants…"
      searchFn={(g, q) => {
        const identity = resolveGrantIdentity(g, patAttribution, userAttribution);
        return (
          identity.primary.toLowerCase().includes(q) ||
          (identity.email?.toLowerCase().includes(q) ?? false) ||
          (identity.ownerName?.toLowerCase().includes(q) ?? false) ||
          (identity.ownerEmail?.toLowerCase().includes(q) ?? false) ||
          g.principal_id.toLowerCase().includes(q) ||
          g.principal_type.toLowerCase().includes(q) ||
          g.scopes.some((s) => s.toLowerCase().includes(q))
        );
      }}
      defaultSortId="principal"
      defaultSortDir="asc"
      emptyMessage="No grants yet."
      filteredEmptyMessage="No grants match your filters."
      toolbarActions={viewToggle}
    />
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
  return (
    <Modal open={!!grant} onClose={onClose} size="sm">
      <ModalHeader title="Delete Grant" />
      <ModalBody>
        <Stack gap="comfortable">
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
        </Stack>
      </ModalBody>
      <ModalFooter>
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
      </ModalFooter>
    </Modal>
  );
}

// ── User picker ───────────────────────────────────────────────────────────────

function UserPicker({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const [users, setUsers] = useState<PATAttributionEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    let cancelled = false;
    const delay = search ? 300 : 0;
    const timer = setTimeout(async () => {
      if (cancelled) return;
      setLoading(true);
      const result = await searchUsersAction(search);
      if (cancelled) return;
      setLoading(false);
      if (result.ok) setUsers(result.data);
    }, delay);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [search]);

  return (
    <section>
      <div className="relative mb-1.5 block">
        <input
          type="text"
          placeholder="Search users…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
      </div>
      <section className="border border-border rounded-lg overflow-hidden max-h-56 overflow-y-auto">
        {loading || users.length === 0 ? (
          <section className="px-3 py-1.5">
            <Text size="caption" color="muted">
              {loading ? 'Loading…' : search ? 'No users match your search' : 'No users found'}
            </Text>
          </section>
        ) : (
          <ul className="list-none m-0 p-0">
            {users.map((u) => {
              const isSelected = value === u.id;
              return (
                <li key={u.id} className="border-b border-border-subtle last:border-b-0">
                  <button
                    type="button"
                    onClick={() => onChange(u.id)}
                    className={[
                      'w-full flex items-center gap-2 px-3 py-1.5 text-left transition-colors cursor-pointer select-none',
                      isSelected ? 'bg-accent/10' : 'hover:bg-surface-overlay',
                    ].join(' ')}
                  >
                    <div
                      className={`flex-1 min-w-0 truncate text-xs leading-none ${isSelected ? 'font-medium text-accent' : 'text-foreground'}`}
                    >
                      {u.display_name || u.email}
                    </div>
                    {u.display_name && u.display_name !== u.email && (
                      <div className="shrink-0 text-[11px] leading-none text-muted">{u.email}</div>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </section>
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
  const [principalType, setPrincipalType] = useState<PrincipalType>('AGENT');
  const [principalId, setPrincipalId] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [scopeSearch, setScopeSearch] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

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
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader title="Create Grant" />
      <ModalBody>
        <Stack gap="comfortable">
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
                  onClick={() => {
                    setPrincipalType(t);
                    setPrincipalId('');
                  }}
                >
                  {t}
                </Button>
              ))}
            </Stack>
          </Stack>

          {/* Principal — user picker for HUMAN, freeform for others */}
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              {principalType === 'HUMAN' ? 'User' : 'Principal ID'}
            </Text>
            {principalType === 'HUMAN' ? (
              <UserPicker value={principalId} onChange={setPrincipalId} />
            ) : (
              <input
                type="text"
                placeholder={principalType === 'AGENT' ? 'Agent UUID' : 'Service identifier'}
                value={principalId}
                onChange={(e) => setPrincipalId(e.target.value)}
                className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              />
            )}
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
          disabled={submitting || !principalId || selectedScopes.length === 0}
        >
          {submitting ? 'Creating...' : 'Create Grant'}
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
