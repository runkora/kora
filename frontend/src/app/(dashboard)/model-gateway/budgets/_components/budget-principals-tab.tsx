/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo } from 'react';
import {
  Badge,
  Callout,
  DataTable,
  Grid,
  Icon,
  PageSectionHeader,
  Progress,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState, MetricCard, StatusBadge } from '@orbit/components';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import type { BudgetCategoryStatus, BudgetPrincipalRow, BudgetPrincipalsView } from '../../_types';
import { TenantCapCard } from './tenant-cap-card';
import { principalDisplay } from './principal-display';

const NEAR_LIMIT_THRESHOLD = 0.8;

// utilisation compares effective_spend_usd (window-matched to the effective
// limit) against the limit. Using current_spend_usd (always monthly) against a
// weekly effective limit would inflate the ratio by 4-5x for weekly principals.
function utilisation(p: BudgetPrincipalRow): number {
  if (!p.has_limit || p.effective_limit_usd <= 0) return 0;
  return p.effective_spend_usd / p.effective_limit_usd;
}

// chipLabel renders one budget source: "role:eng · $12.00 / $50.00/wk · pool".
function chipLabel(cat: BudgetCategoryStatus): string {
  const parts = [`${cat.source}:${cat.id}`];
  if (cat.limit_usd > 0) {
    parts.push(
      `${fmtUsd(cat.spend_usd)} / ${fmtUsd(cat.limit_usd)}${fmtWindowSuffix(cat.reset_cadence)}`,
    );
  }
  if (cat.per_run_limit_usd) {
    parts.push(`${fmtUsd(cat.per_run_limit_usd)}/run`);
  }
  if (cat.pooled) {
    parts.push('pool');
  }
  return parts.join(' · ');
}

interface BudgetPrincipalsTabProps {
  principals: BudgetPrincipalsView | null;
}

export function BudgetPrincipalsTab({ principals }: BudgetPrincipalsTabProps) {
  const rows = useMemo(() => principals?.data ?? [], [principals]);

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
        locked: true,
        sortable: true,
        sortAccessor: (p) => principalDisplay(p.principal_id, p.principal_type, p).primary,
        cell: (p) => {
          const display = principalDisplay(p.principal_id, p.principal_type, p);
          return (
            <Stack gap="tight">
              <Stack direction="horizontal" align="center" gap="tight">
                <Text size="caption" weight="emphasis">
                  {display.primary}
                </Text>
                {p.principal_type && (
                  <Badge variant="secondary" size="sm">
                    {p.principal_type.toLowerCase()}
                  </Badge>
                )}
              </Stack>
              {display.secondary && (
                <Text size="caption" color="subtle" mono>
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
        cell: (p) =>
          p.categories.length > 0 ? (
            <Stack direction="horizontal" gap="tight" wrap>
              {p.categories.map((cat) => (
                <Badge key={`${cat.source}:${cat.id}`} variant="elevated" size="sm">
                  {chipLabel(cat)}
                </Badge>
              ))}
            </Stack>
          ) : (
            <Text size="caption" color="subtle">
              —
            </Text>
          ),
      },
      {
        id: 'limit',
        header: 'Effective Limit',
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
        header: 'Spend (month-to-date)',
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
          if (!p.has_limit) return <StatusBadge status="idle" label="No budget" />;
          if (p.spend_limit_reached) return <StatusBadge status="error" label="Blocked" />;
          if (utilisation(p) >= NEAR_LIMIT_THRESHOLD)
            return <StatusBadge status="degraded" label="Near limit" />;
          return <StatusBadge status="healthy" label="OK" />;
        },
      },
      {
        id: 'lastActive',
        header: 'Last Active',
        sortable: true,
        sortAccessor: (p) => p.last_active_at ?? '',
        cell: (p) => (
          <Text size="caption" color="subtle">
            {p.last_active_at ? fmtDate(p.last_active_at) : '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  return (
    <Stack gap="comfortable">
      <PageSectionHeader
        title="Principals"
        description={`Spend against budget for the current period${principals ? ` (${principals.period})` : ''}.`}
      />

      <TenantCapCard cap={principals?.tenant_cap ?? null} />

      <Grid cols={{ base: 1, sm: 2, lg: 4 }} gap="comfortable">
        <MetricCard
          icon={<Icon name="dollar-sign" />}
          label="Period spend"
          value={fmtUsd(kpis.periodSpend)}
        />
        <MetricCard
          icon={<Icon name="users" />}
          label="Budgeted principals"
          value={kpis.budgeted}
        />
        <MetricCard
          icon={<Icon name="triangle-alert" />}
          label="Near limit"
          value={kpis.nearLimit}
        />
        <MetricCard icon={<Icon name="circle-x" />} label="Blocked" value={kpis.blocked} />
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
        />
      )}
    </Stack>
  );
}
