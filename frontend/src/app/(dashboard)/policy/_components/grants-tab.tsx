'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Callout,
  Alert,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Grid,
  Icon,
  Input,
  Stack,
  Text,
  PageSectionHeader,
  EmptyState,
  ConfirmDialog,
} from '@runkora/hades';
import { SmartTable } from '@runkora/components';
import type { DataTableColumn, ScopeOption } from '@runkora/components';
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import type { BudgetAssignment, Grant, PrincipalType } from '../_types';
import { resolveGrantIdentity } from '../_utils/grant-identity';
import { isGrantEditable } from '../_utils/grant-owner';

// budgetSummary renders "$50.00/mo · $5.00/run" for a grant budget.
function budgetSummary(budget: BudgetAssignment): string {
  const parts: string[] = [];
  if (budget.limit_usd) {
    parts.push(`${fmtUsd(budget.limit_usd)}${fmtWindowSuffix(budget.reset_cadence)}`);
  }
  if (budget.per_run_limit_usd) {
    parts.push(`${fmtUsd(budget.per_run_limit_usd)}/run`);
  }
  return parts.join(' · ');
}
import { deleteGrantAction } from '../_actions/grants';
import { useHasScope } from '../../_components/permissions-provider';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import {
  PrincipalBadge,
  principalLabel,
  ScopeChip,
  ScopeChipList,
  scopeColumnFilter,
} from '@runkora/components';
import { FilterChip } from '@runkora/components';
import { CreateGrantWizard } from './create-grant-wizard';
import { GrantDetailModal } from './grant-detail-modal';
import { GrantOwnerLink } from './grant-owner-link';
import { ViewToggle } from '@runkora/components';
import { PermissionHint } from '@runkora/components';

const PRINCIPAL_TYPES: PrincipalType[] = ['AGENT', 'HUMAN', 'PAT', 'SERVICE'];

function grantMatches(
  g: Grant,
  q: string,
  patAttribution: Record<string, PATGrantAttribution>,
  userAttribution: Record<string, PATAttributionEntry>,
): boolean {
  const identity = resolveGrantIdentity(g, patAttribution, userAttribution);
  return (
    identity.primary.toLowerCase().includes(q) ||
    (identity.email?.toLowerCase().includes(q) ?? false) ||
    (identity.ownerName?.toLowerCase().includes(q) ?? false) ||
    (identity.ownerEmail?.toLowerCase().includes(q) ?? false) ||
    g.principal_id.toLowerCase().includes(q) ||
    g.principal_type.toLowerCase().includes(q) ||
    principalLabel(g.principal_type).toLowerCase().includes(q) ||
    g.scopes.some((s) => s.toLowerCase().includes(q))
  );
}

// ── Props ────────────────────────────────────────────────────────────────────

interface GrantsTabProps {
  initialGrants: Grant[];
  availableScopes: string[];
  /** The vocabulary entries behind `availableScopes`, so the wizard can group and describe them. */
  scopeOptions?: ScopeOption[];
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
  scopeOptions,
  patAttribution,
  userAttribution,
  initialError,
  page,
  limit,
  total,
}: GrantsTabProps) {
  const router = useRouter();
  const canWrite = useHasScope('platform:grants:write');
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
  const [selectedGrant, setSelectedGrant] = useState<Grant | null>(null);
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
      result = result.filter((g) => grantMatches(g, q, patAttribution, userAttribution));
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
      <PageSectionHeader
        title="Grants"
        description="Permission grants assigned to principals in your tenant."
        action={
          <PermissionHint allowed={canWrite} action="create grants">
            <Button size="sm" disabled={!canWrite} onClick={() => setCreating(true)}>
              <Icon name="plus" />
              Create Grant
            </Button>
          </PermissionHint>
        }
      />

      {/* Info box */}
      <Callout>
        <Text size="caption" color="muted">
          Grants assign specific scopes directly to a principal. Unlike roles (which map OIDC claims
          to scopes), grants are explicit: they apply regardless of how the principal authenticates.
          You create and delete grants for people here. Agent, trigger and token grants are managed
          by the agent, trigger or token they belong to.
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
            <ViewToggle viewMode={viewMode} onToggle={setViewMode} />
          </Stack>

          <Stack direction="horizontal" align="center" justify="between">
            <Stack direction="horizontal" align="center" gap="snug" wrap>
              <FilterChip
                label="All"
                count={grants.length}
                active={typeFilter === 'all'}
                onClick={() => setTypeFilter('all')}
              />
              {PRINCIPAL_TYPES.map((type) => (
                <FilterChip
                  key={type}
                  label={principalLabel(type)}
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
              description="Grants assign scopes directly to principals. Create your first grant to give a person access to specific operations."
              action={
                <PermissionHint allowed={canWrite} action="create grants">
                  <Button size="sm" disabled={!canWrite} onClick={() => setCreating(true)}>
                    <Icon name="plus" />
                    Create Grant
                  </Button>
                </PermissionHint>
              }
            />
          )
        ) : (
          <GrantCardGrid
            grants={filtered}
            patAttribution={patAttribution}
            userAttribution={userAttribution}
            onDelete={setDeleteCandidate}
            canWrite={canWrite}
          />
        )
      ) : (
        <GrantTable
          grants={grants}
          patAttribution={patAttribution}
          userAttribution={userAttribution}
          onSelect={setSelectedGrant}
          onDelete={setDeleteCandidate}
          canWrite={canWrite}
          viewToggle={<ViewToggle viewMode={viewMode} onToggle={setViewMode} />}
        />
      )}

      {/* Pagination */}
      {total > limit && <GrantsPagination page={page} limit={limit} total={total} />}

      {/* Delete confirmation modal */}
      <ConfirmDialog
        open={!!deleteCandidate}
        title="Delete Grant"
        description="Remove this grant? This action cannot be undone."
        confirmLabel="Delete"
        size="md"
        confirming={deletingId === deleteCandidate?.id}
        error={deleteError}
        onConfirm={() => deleteCandidate && void handleDeleteConfirm(deleteCandidate.id)}
        onClose={() => {
          setDeleteCandidate(null);
          setDeleteError(null);
        }}
      >
        {deleteCandidate && (
          <Card subtle>
            <CardContent padded compact>
              <Stack gap="snug">
                <Stack direction="horizontal" align="center" gap="snug">
                  <PrincipalBadge type={deleteCandidate.principal_type} />
                  <Text size="caption" mono weight="emphasis">
                    {deleteCandidate.principal_id}
                  </Text>
                </Stack>
                <Stack direction="horizontal" gap="tight" wrap>
                  {deleteCandidate.scopes.map((s) => (
                    <ScopeChip key={s} scope={s} />
                  ))}
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        )}
      </ConfirmDialog>

      <GrantDetailModal
        grant={selectedGrant}
        identity={
          selectedGrant
            ? resolveGrantIdentity(selectedGrant, patAttribution, userAttribution)
            : null
        }
        onClose={() => setSelectedGrant(null)}
      />

      {creating && (
        <CreateGrantWizard
          availableScopes={availableScopes}
          scopeOptions={scopeOptions}
          onClose={() => setCreating(false)}
          onCreated={() => router.refresh()}
        />
      )}
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
        <Stack align="center" paddingInline="snug">
          <Text size="caption" color="muted">
            Page {page} of {totalPages}
          </Text>
        </Stack>
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

// ── Card grid view ──────────────────────────────────────────────────────────

function GrantCardGrid({
  grants,
  patAttribution,
  userAttribution,
  onDelete,
  canWrite,
}: {
  grants: Grant[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  onDelete: (grant: Grant) => void;
  canWrite: boolean;
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
              <ScopeChipList scopes={grant.scopes} max={8} />
            </CardContent>

            <CardFooter compact justify="between">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="subtle">
                  {grant.scopes.length} {grant.scopes.length === 1 ? 'scope' : 'scopes'}
                </Text>
                {grant.budget && (
                  <Text size="caption" color="muted">
                    {budgetSummary(grant.budget)}
                  </Text>
                )}
              </Stack>
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="subtle">
                  Created {fmtDate(grant.created_at)}
                </Text>
                {isGrantEditable(grant) ? (
                  <PermissionHint allowed={canWrite} action="delete grants">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      disabled={!canWrite}
                      onClick={() => onDelete(grant)}
                      aria-label="Delete grant"
                    >
                      <Icon name="trash" size="2xs" />
                    </Button>
                  </PermissionHint>
                ) : (
                  <GrantOwnerLink grant={grant} />
                )}
              </Stack>
            </CardFooter>
          </Card>
        );
      })}
    </Grid>
  );
}

// ── Table view ──────────────────────────────────────────────────────────────

const TYPE_OPTIONS = PRINCIPAL_TYPES.map((value) => ({ value, label: principalLabel(value) }));

function GrantTable({
  grants,
  patAttribution,
  userAttribution,
  onSelect,
  onDelete,
  canWrite,
  viewToggle,
}: {
  grants: Grant[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  onSelect: (grant: Grant) => void;
  onDelete: (grant: Grant) => void;
  canWrite: boolean;
  viewToggle: React.ReactNode;
}) {
  const columns = useMemo<DataTableColumn<Grant>[]>(
    () => [
      {
        id: 'principal',
        header: 'Principal',
        minWidth: '7rem',
        gridWidth: 'minmax(0, 2fr)',
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
        minWidth: '5rem',
        gridWidth: 'minmax(0, 1.2fr)',
        cell: (g) => {
          const { rawId, primary } = resolveGrantIdentity(g, patAttribution, userAttribution);
          const id = rawId ?? primary;
          return (
            <Stack noWrap>
              <Text size="caption" color="subtle" mono>
                {id}
              </Text>
            </Stack>
          );
        },
      },
      {
        id: 'type',
        header: 'Type',
        minWidth: '4.5rem',
        gridWidth: '6rem',
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
        minWidth: '16rem',
        gridWidth: 'minmax(0, 3fr)',
        wrap: true,
        filter: scopeColumnFilter(grants, (g) => g.scopes),
        cell: (g) => <ScopeChipList scopes={g.scopes} maxRows={2} />,
      },
      {
        id: 'budget',
        header: 'Budget',
        minWidth: '5.5rem',
        gridWidth: '6rem',
        cell: (g) =>
          g.budget ? (
            <Text size="caption" color="muted">
              {budgetSummary(g.budget)}
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
        minWidth: '5.5rem',
        gridWidth: '6rem',
        sortable: true,
        sortAccessor: (g) => g.created_at,
        cell: (g) => (
          <Stack noWrap>
            <Text size="caption" color="subtle">
              {fmtDate(g.created_at)}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '7rem',
        cell: (g) =>
          isGrantEditable(g) ? (
            <PermissionHint allowed={canWrite} action="delete grants">
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite}
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete(g);
                }}
                aria-label="Delete grant"
              >
                <Icon name="trash" size="2xs" />
              </Button>
            </PermissionHint>
          ) : (
            <GrantOwnerLink grant={g} />
          ),
      },
    ],
    [onDelete, canWrite, patAttribution, userAttribution, grants],
  );

  return (
    <SmartTable
      tableId="policyGrants"
      data={grants}
      columns={columns}
      getRowKey={(g) => g.id}
      noun="grants"
      onRowClick={onSelect}
      rowLabel={(g) =>
        `View details for ${resolveGrantIdentity(g, patAttribution, userAttribution).primary}`
      }
      searchPlaceholder="Search grants…"
      searchFn={(g, q) => grantMatches(g, q, patAttribution, userAttribution)}
      defaultSortId="principal"
      defaultSortDir="asc"
      emptyMessage="No grants yet."
      filteredEmptyMessage="No grants match your filters."
      toolbarActions={viewToggle}
    />
  );
}
