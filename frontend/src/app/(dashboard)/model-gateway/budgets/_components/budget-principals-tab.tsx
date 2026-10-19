/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo, useState } from 'react';
import {
  Badge,
  Callout,
  DataTable,
  Grid,
  Icon,
  PageSectionHeader,
  Progress,
  Stack,
  StatCard,
  Text,
  EmptyState,
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { ClampedList, StatusBadge } from '@runkora/components';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import type { BudgetCategoryStatus, BudgetPrincipalRow, BudgetPrincipalsView } from '../../_types';
import { TenantCapCard } from './tenant-cap-card';
import { principalDisplay } from './principal-display';
import { BudgetPrincipalDetailModal } from './budget-principal-detail-modal';
import {
  chipLabel,
  chipTitle,
  chipVariant,
  NEAR_LIMIT_THRESHOLD,
  principalStatus,
  utilisation,
} from './budget-principal-format';

// A source is a role or a grant; the pair is what tells two apart.
const sourceKey = (cat: BudgetCategoryStatus) => `${cat.source}:${cat.id}`;

interface BudgetPrincipalsTabProps {
  principals: BudgetPrincipalsView | null;
}

export function BudgetPrincipalsTab({ principals }: BudgetPrincipalsTabProps) {
  const rows = useMemo(() => principals?.data ?? [], [principals]);
  const [selectedPrincipal, setSelectedPrincipal] = useState<BudgetPrincipalRow | null>(null);

  const kpis = useMemo(() => {
    const budgeted = rows.filter((p) => p.has_limit);
    return {
      periodSpend: rows.reduce((sum, p) => sum + p.current_spend_usd, 0),
      budgeted: budgeted.length,
      nearLimit: budgeted.filter(
        (p) => !p.spend_limit_reached && utilisation(p) >= NEAR_LIMIT_THRESHOLD,
      ).length,
      blocked: budgeted.filter((p) => p.spend_limit_reached).length,
    };
  }, [rows]);

  const columns = useMemo<DataTableColumn<BudgetPrincipalRow>[]>(
    () => [
      {
        id: 'principal',
        header: 'Principal',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '10rem',
        locked: true,
        sortable: true,
        sortAccessor: (p) => principalDisplay(p.principal_id, p.principal_type, p).primary,
        cell: (p) => {
          const display = principalDisplay(p.principal_id, p.principal_type, p);
          return (
            <Stack gap="tight" minWidth="none">
              <Stack direction="horizontal" align="center" gap="tight">
                <Text size="caption" weight="emphasis" truncate title={display.primary}>
                  {display.primary}
                </Text>
                {p.principal_type && (
                  <Badge variant="secondary" size="sm">
                    {p.principal_type.toLowerCase()}
                  </Badge>
                )}
              </Stack>
              {display.secondary && (
                <Text size="caption" color="subtle" mono truncate title={display.secondary}>
                  {display.secondary}
                </Text>
              )}
            </Stack>
          );
        },
      },
      {
        id: 'sources',
        header: 'Budget Sources',
        gridWidth: 'minmax(0, 1.5fr)',
        minWidth: '8rem',
        wrap: true,
        filter: {
          mode: 'client',
          searchable: true,
          mono: true,
          options: [...new Set(rows.flatMap((p) => p.categories.map(sourceKey)))]
            .sort()
            .map((key) => ({ value: key, label: key })),
          filterFn: (p, selected) => p.categories.some((cat) => selected.has(sourceKey(cat))),
        },
        cell: (p) =>
          p.categories.length > 0 ? (
            <ClampedList
              items={p.categories}
              getKey={sourceKey}
              renderItem={(cat) => (
                <Badge variant={chipVariant(cat)} size="sm" title={chipTitle(cat)}>
                  {chipLabel(cat)}
                </Badge>
              )}
              noun="budget sources"
              maxRows={2}
            />
          ) : (
            <Text size="caption" color="subtle">
              —
            </Text>
          ),
      },
      {
        id: 'limit',
        header: 'Effective Limit',
        gridWidth: '8rem',
        minWidth: '6rem',
        sortable: true,
        sortAccessor: (p) => p.effective_limit_usd,
        cell: (p) =>
          p.has_limit ? (
            <Text size="caption" mono>
              {fmtUsd(p.effective_limit_usd)}
              {fmtWindowSuffix(p.effective_reset_cadence ?? 'monthly')}
            </Text>
          ) : (
            <Text size="caption" color="subtle">
              No budget
            </Text>
          ),
      },
      {
        id: 'spend',
        header: 'Spend (MTD)',
        gridWidth: '8rem',
        minWidth: '8rem',
        headerTooltip: 'Month-to-date spend for the current period',
        sortable: true,
        sortAccessor: (p) => p.current_spend_usd,
        cell: (p) => (
          <Text size="caption" mono>
            {fmtUsd(p.current_spend_usd)}
          </Text>
        ),
      },
      {
        id: 'utilisation',
        header: 'Utilisation',
        gridWidth: '8rem',
        minWidth: '5.5rem',
        sortable: true,
        sortAccessor: utilisation,
        cell: (p) => {
          if (!p.has_limit) {
            return (
              <Text size="caption" color="subtle">
                —
              </Text>
            );
          }
          const pct = utilisation(p);
          const variant = p.spend_limit_reached
            ? 'error'
            : pct >= NEAR_LIMIT_THRESHOLD
              ? 'degraded'
              : 'healthy';
          return (
            <Progress
              value={p.effective_spend_usd}
              max={p.effective_limit_usd}
              label={`Budget utilisation for ${p.principal_id}`}
              variant={variant}
              showValue
            />
          );
        },
      },
      {
        id: 'status',
        header: 'Status',
        gridWidth: '6rem',
        minWidth: '5rem',
        sortable: true,
        sortAccessor: (p) =>
          p.spend_limit_reached
            ? 3
            : utilisation(p) >= NEAR_LIMIT_THRESHOLD
              ? 2
              : p.has_limit
                ? 1
                : 0,
        cell: (p) => {
          const status = principalStatus(p);
          return <StatusBadge status={status.variant} label={status.label} />;
        },
      },
      {
        id: 'lastActive',
        header: 'Last Active',
        gridWidth: '8rem',
        minWidth: '6.5rem',
        sortable: true,
        sortAccessor: (p) => p.last_active_at ?? '',
        cell: (p) => (
          <Text size="caption" color="subtle">
            {p.last_active_at ? fmtDate(p.last_active_at) : '—'}
          </Text>
        ),
      },
    ],
    [rows],
  );

  return (
    <Stack gap="comfortable">
      <PageSectionHeader
        title="Principals"
        description={`Spend against budget for the current period${principals ? ` (${principals.period})` : ''}.`}
      />

      <TenantCapCard cap={principals?.tenant_cap ?? null} />

      <Grid cols={{ base: 1, sm: 2, lg: 4 }} gap="comfortable">
        <StatCard
          icon={<Icon name="dollar-sign" size="xs" />}
          label="Period spend"
          value={fmtUsd(kpis.periodSpend)}
        />
        <StatCard
          icon={<Icon name="users" size="xs" />}
          label="Budgeted principals"
          value={kpis.budgeted}
        />
        <StatCard
          icon={<Icon name="triangle-alert" size="xs" />}
          label="Near limit"
          value={kpis.nearLimit}
        />
        <StatCard icon={<Icon name="circle-x" size="xs" />} label="Blocked" value={kpis.blocked} />
      </Grid>

      <Callout>
        <Text size="caption" color="muted">
          Role budgets are attributed from inference activity: a principal holding a budgeted role
          appears here after their first request of the period. The effective limit is the highest
          limit across all applicable role and grant budgets.
        </Text>
      </Callout>

      {rows.length === 0 ? (
        <EmptyState
          icon="dollar-sign"
          title="No budget activity"
          description="No principals have budgets or spend this period. Assign budgets on the Role Budgets and Grant Budgets tabs."
        />
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(p) => p.principal_id}
          noun="principals"
          searchPlaceholder="Search principals…"
          searchFn={(p, q) =>
            p.principal_id.toLowerCase().includes(q) ||
            (p.display_name?.toLowerCase().includes(q) ?? false) ||
            (p.email?.toLowerCase().includes(q) ?? false) ||
            p.categories.some((c) => c.id.toLowerCase().includes(q))
          }
          defaultSortId="utilisation"
          defaultSortDir="desc"
          emptyMessage="No budget activity this period."
          filteredEmptyMessage="No principals match your search."
          onRowClick={setSelectedPrincipal}
          rowLabel={(p) =>
            `View details for ${principalDisplay(p.principal_id, p.principal_type, p).primary}`
          }
        />
      )}

      <BudgetPrincipalDetailModal
        principal={selectedPrincipal}
        onClose={() => setSelectedPrincipal(null)}
      />
    </Stack>
  );
}
