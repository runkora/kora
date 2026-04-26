'use client';

import { useCallback, useEffect, useState } from 'react';
import { Badge, Button, DataTable, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ApiRoute } from '@orbit/lib/routing';
import type {
  AnalyticsDays,
  AnalyticsPrincipalsPageResponse,
  PrincipalUsageRow,
} from '../../_types';
import { fmtCost, fmtDate, fmtNum } from '@orbit/utils';

// ── Hook ──────────────────────────────────────────────────────────────────────

function useAnalyticsPrincipals(days: AnalyticsDays) {
  const [page, setPage] = useState(1);
  const [data, setData] = useState<AnalyticsPrincipalsPageResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (d: AnalyticsDays, p: number) => {
    setLoading(true);
    setError(null);
    try {
      const r = await fetch(ApiRoute.analyticsPrincipals.GET({ days: d, page: p }));
      if (!r.ok) throw new Error(r.statusText);
      setData((await r.json()) as AnalyticsPrincipalsPageResponse);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial fetch on mount. Days changes are handled by the parent passing
  // key={days}, which remounts this component and resets page state.
  useEffect(() => {
    void load(days, 1);
  }, [days, load]);

  const goToPage = useCallback(
    (p: number) => {
      setPage(p);
      void load(days, p);
    },
    [days, load],
  );

  const totalPages = data ? Math.ceil(data.total / data.limit) : 0;

  return { data, loading, error, page, goToPage, totalPages };
}

// ── Columns ───────────────────────────────────────────────────────────────────

const COLUMNS: DataTableColumn<PrincipalUsageRow>[] = [
  {
    id: 'user',
    header: 'User',
    locked: true,
    cell: (row) => (
      <Stack gap="none">
        <Text size="supporting" color="foreground">
          {row.display_name || row.email || row.principal_id}
        </Text>
        {(row.display_name || row.email) && (
          <Text size="caption" color="subtle" mono>
            {row.email ?? row.principal_id}
          </Text>
        )}
      </Stack>
    ),
  },
  {
    id: 'requests',
    header: 'Requests',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.request_count,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {fmtNum(row.request_count)}
      </Text>
    ),
  },
  {
    id: 'totalTokens',
    header: 'Total tokens',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.total_tokens,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {fmtNum(row.total_tokens)}
      </Text>
    ),
  },
  {
    id: 'promptTokens',
    header: 'Input tokens',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.prompt_tokens,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {fmtNum(row.prompt_tokens)}
      </Text>
    ),
  },
  {
    id: 'completionTokens',
    header: 'Output tokens',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.completion_tokens,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {fmtNum(row.completion_tokens)}
      </Text>
    ),
  },
  {
    id: 'cacheTokens',
    header: 'Cache tokens',
    align: 'right',
    defaultVisible: false,
    sortable: true,
    sortAccessor: (row) => row.cache_read_tokens,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {fmtNum(row.cache_read_tokens)}
      </Text>
    ),
  },
  {
    id: 'cost',
    header: 'Cost',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.estimated_cost_usd,
    cell: (row) => (
      <Text size="supporting" color="foreground" mono align="end">
        {fmtCost(row.estimated_cost_usd)}
      </Text>
    ),
  },
  {
    id: 'errorRate',
    header: 'Error rate',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.error_rate,
    cell: (row) => {
      const pct = (row.error_rate * 100).toFixed(1);
      const variant =
        row.error_rate > 0.1 ? 'error' : row.error_rate > 0.02 ? 'degraded' : 'secondary';
      return (
        <Badge variant={variant} size="sm">
          {pct}%
        </Badge>
      );
    },
  },
  {
    id: 'latency',
    header: 'Avg latency',
    align: 'right',
    sortable: true,
    sortAccessor: (row) => row.avg_latency_ms,
    cell: (row) => (
      <Text size="supporting" color="muted" mono align="end">
        {Math.round(row.avg_latency_ms)}ms
      </Text>
    ),
  },
  {
    id: 'lastActive',
    header: 'Last active',
    sortable: true,
    sortAccessor: (row) => new Date(row.last_active_at).getTime(),
    cell: (row) => (
      <Text size="supporting" color="muted" mono>
        {fmtDate(row.last_active_at)}
      </Text>
    ),
  },
];

// ── Component ─────────────────────────────────────────────────────────────────

// Parent should pass key={days} so this component remounts when the time
// window changes, resetting page state and triggering a fresh fetch.
export function AnalyticsPrincipalsTable({ days }: { days: AnalyticsDays }) {
  const { data, loading, error, page, goToPage, totalPages } = useAnalyticsPrincipals(days);

  return (
    <Stack gap="comfortable">
      {error && (
        <Text size="supporting" color="status-error">
          {error}
        </Text>
      )}

      <section
        className={loading ? 'pointer-events-none opacity-50 transition-opacity' : undefined}
      >
        <DataTable
          data={data?.data ?? []}
          columns={COLUMNS}
          getRowKey={(row) => row.principal_id}
          noun="users"
          searchPlaceholder="Search email or user ID…"
          searchFn={(row, q) =>
            row.principal_id.toLowerCase().includes(q) ||
            (row.email?.toLowerCase().includes(q) ?? false) ||
            (row.display_name?.toLowerCase().includes(q) ?? false)
          }
          defaultSortId="cost"
          defaultSortDir="desc"
          emptyMessage="No inference activity in this period."
          filteredEmptyMessage="No users match your search."
        />
      </section>

      {totalPages > 1 && (
        <Stack direction="horizontal" gap="tight" justify="end" align="center">
          <Text size="caption" color="subtle">
            Page {page} of {totalPages}
          </Text>
          <Button
            variant="outline"
            size="sm"
            onClick={() => goToPage(Math.max(1, page - 1))}
            disabled={page <= 1 || loading}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => goToPage(Math.min(totalPages, page + 1))}
            disabled={page >= totalPages || loading}
          >
            Next
          </Button>
        </Stack>
      )}
    </Stack>
  );
}
