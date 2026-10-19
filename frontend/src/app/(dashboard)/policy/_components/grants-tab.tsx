'use client';

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Callout,
  Alert,
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
  RadioCard,
  RadioGroup,
  Stack,
  Text,
  Toggle,
  ToggleRow,
  PageSectionHeader,
  EmptyState,
  ConfirmDialog,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import { SmartTable } from '@runkora/components';
import type { DataTableColumn } from '@runkora/components';
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import type { BudgetAssignment, Grant, PrincipalType } from '../_types';
import { resolveGrantIdentity } from '../_utils/grant-identity';

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
import { createGrantAction, deleteGrantAction, searchUsersAction } from '../_actions/grants';
import { useHasScope } from '../../_components/permissions-provider';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import { PrincipalBadge, ScopeChip, ScopeChipList, scopeColumnFilter } from '@runkora/components';
import { ScopePicker } from '@runkora/components';
import { FilterChip } from './filter-chip';
import { GrantDetailModal } from './grant-detail-modal';
import { ViewToggle } from './view-toggle';

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
      <PageSectionHeader
        title="Grants"
        description="Permission grants assigned to principals in your tenant."
        action={
          canWrite ? (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Icon name="plus" />
              Create Grant
            </Button>
          ) : undefined
        }
      />

      {/* Info box */}
      <Callout>
        <Text size="caption" color="muted">
          Grants assign specific scopes directly to a principal. Unlike roles (which map OIDC claims
          to scopes), grants are explicit: they apply regardless of how the principal authenticates.
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
          onSelect={setSelectedGrant}
          onDelete={canWrite ? setDeleteCandidate : undefined}
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

      {/* Create grant modal */}
      <GrantDetailModal
        grant={selectedGrant}
        identity={
          selectedGrant
            ? resolveGrantIdentity(selectedGrant, patAttribution, userAttribution)
            : null
        }
        onClose={() => setSelectedGrant(null)}
      />

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
  onSelect,
  onDelete,
  viewToggle,
}: {
  grants: Grant[];
  patAttribution: Record<string, PATGrantAttribution>;
  userAttribution: Record<string, PATAttributionEntry>;
  onSelect: (grant: Grant) => void;
  onDelete?: (grant: Grant) => void;
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
        gridWidth: '3rem',
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
    [onDelete, patAttribution, userAttribution, grants],
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
    <Stack gap="none">
      <Stack marginBottom="tight">
        <Input
          type="text"
          placeholder="Search users…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </Stack>
      <Card maxHeight="scroll-sm" overflowY="auto" border="default" divide>
        {loading || users.length === 0 ? (
          <Stack paddingBlock="tight" paddingInline="snug">
            <Text size="caption" color="muted">
              {loading ? 'Loading…' : search ? 'No users match your search' : 'No users found'}
            </Text>
          </Stack>
        ) : (
          users.map((u) => {
            const isSelected = value === u.id;
            return (
              <Button
                key={u.id}
                variant="ghost"
                shape="square"
                size="sm"
                selected={isSelected}
                fullWidth
                onClick={() => onChange(u.id)}
              >
                <Stack direction="horizontal" grow align="center" justify="between" minWidth="none">
                  <Text
                    size="micro"
                    truncate
                    color={isSelected ? 'accent' : 'foreground'}
                    weight={isSelected ? 'emphasis' : undefined}
                  >
                    {u.display_name || u.email}
                  </Text>
                  {u.display_name && u.display_name !== u.email && (
                    <Text size="micro" color="muted">
                      {u.email}
                    </Text>
                  )}
                </Stack>
              </Button>
            );
          })
        )}
      </Card>
    </Stack>
  );
}

// ── Create modal ─────────────────────────────────────────────────────────────

const PRINCIPAL_TYPE_OPTIONS: {
  value: PrincipalType;
  icon: IconName;
  description: string;
}[] = [
  { value: 'AGENT', icon: 'bot', description: 'Autonomous workload' },
  { value: 'HUMAN', icon: 'user', description: 'A person in the tenant' },
  { value: 'SERVICE', icon: 'server', description: 'External system' },
];

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
  const [budgetEnabled, setBudgetEnabled] = useState(false);
  const [limitUSD, setLimitUSD] = useState('');
  const [cadence, setCadence] = useState('monthly');
  const [perRunUSD, setPerRunUSD] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const selectPrincipalType = useCallback((value: string) => {
    setPrincipalType(value as PrincipalType);
    setPrincipalId('');
  }, []);

  const handleSubmit = useCallback(async () => {
    const trimmedId = principalId.trim();
    if (!trimmedId || selectedScopes.length === 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const budget =
      budgetEnabled && (limitUSD || perRunUSD)
        ? {
            reset_cadence: cadence,
            ...(limitUSD ? { limit_usd: parseFloat(limitUSD) } : {}),
            ...(perRunUSD ? { per_run_limit_usd: parseFloat(perRunUSD) } : {}),
          }
        : null;
    const result = await createGrantAction({
      principal_type: principalType,
      principal_id: trimmedId,
      scopes: selectedScopes,
      budget,
    });
    if (result.ok) {
      setPrincipalId('');
      setSelectedScopes([]);
      setBudgetEnabled(false);
      setLimitUSD('');
      setPerRunUSD('');
      onCreated();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [
    principalType,
    principalId,
    selectedScopes,
    budgetEnabled,
    limitUSD,
    cadence,
    perRunUSD,
    onCreated,
  ]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader title="Create Grant" />
      <ModalBody flushY constrained>
        <Stack gap="comfortable">
          {/* Principal type */}
          <Stack gap="tight">
            <Eyebrow>Principal Type</Eyebrow>
            <RadioGroup
              value={principalType}
              onValueChange={selectPrincipalType}
              aria-label="Principal type"
            >
              <Grid cols={3} gap="cozy">
                {PRINCIPAL_TYPE_OPTIONS.map((option) => (
                  <RadioCard
                    key={option.value}
                    value={option.value}
                    selected={principalType === option.value}
                    onSelect={selectPrincipalType}
                    icon={option.icon}
                    title={option.value}
                    description={option.description}
                  />
                ))}
              </Grid>
            </RadioGroup>
          </Stack>

          {/* Principal — user picker for HUMAN, freeform for others */}
          <Stack gap="tight">
            <Eyebrow>{principalType === 'HUMAN' ? 'User' : 'Principal ID'}</Eyebrow>
            {principalType === 'HUMAN' ? (
              <UserPicker value={principalId} onChange={setPrincipalId} />
            ) : (
              <Input
                type="text"
                placeholder={principalType === 'AGENT' ? 'Agent UUID' : 'Service identifier'}
                value={principalId}
                onChange={(e) => setPrincipalId(e.target.value)}
              />
            )}
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
              description="Spend ceiling on this grant"
              htmlFor="grant-budget-toggle"
              control={
                <Toggle
                  id="grant-budget-toggle"
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
                <Stack direction="horizontal" align="center" gap="snug">
                  <Input
                    type="number"
                    placeholder="e.g. 5"
                    startAdornment="$"
                    value={perRunUSD}
                    onChange={(e) => setPerRunUSD(e.target.value)}
                  />
                  <Text size="caption" color="muted">
                    / run (agents)
                  </Text>
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
          disabled={submitting || !principalId || selectedScopes.length === 0}
        >
          {submitting ? 'Creating...' : 'Create Grant'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
