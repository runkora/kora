'use client';

import { useState } from 'react';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  DataTable,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { DAILY_REQUESTS, type DailyRequestRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';
import { fmtNumCompact } from '@orbit/utils';

// ── Helpers ───────────────────────────────────────────────────────────────────

interface RequestRow {
  period: string;
  total: number;
  successful: number;
  errored: number;
}

function buildRows(
  data: DailyRequestRow[],
  granularity: Granularity,
  period: Period,
): RequestRow[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      total: r.total,
      successful: r.successful,
      errored: r.errored,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      total: rows.reduce((s, r) => s + r.total, 0),
      successful: rows.reduce((s, r) => s + r.successful, 0),
      errored: rows.reduce((s, r) => s + r.errored, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    total: rows.reduce((s, r) => s + r.total, 0),
    successful: rows.reduce((s, r) => s + r.successful, 0),
    errored: rows.reduce((s, r) => s + r.errored, 0),
  }));
}

const COLUMNS: DataTableColumn<RequestRow>[] = [
  {
    id: 'period',
    header: 'Period',
    gridWidth: '1fr',
    cell: (row) => (
      <Text size="supporting" mono>
        {row.period}
      </Text>
    ),
  },
  {
    id: 'total',
    header: 'Total',
    gridWidth: '110px',
    align: 'right',
    cell: (row) => (
      <Text size="supporting" mono tabular weight="emphasis" align="end">
        {fmtNumCompact(row.total)}
      </Text>
    ),
  },
  {
    id: 'successful',
    header: 'Successful',
    gridWidth: '110px',
    align: 'right',
    cell: (row) => (
      <Text size="supporting" mono tabular color="status-healthy" align="end">
        {fmtNumCompact(row.successful)}
      </Text>
    ),
  },
  {
    id: 'errored',
    header: 'Errored',
    gridWidth: '110px',
    align: 'right',
    cell: (row) => (
      <Text
        size="supporting"
        mono
        tabular
        color={row.errored > 0 ? 'status-error' : 'muted'}
        align="end"
      >
        {fmtNumCompact(row.errored)}
      </Text>
    ),
  },
  {
    id: 'errorRate',
    header: 'Error rate',
    gridWidth: '100px',
    align: 'right',
    cell: (row) => {
      const errorRate = ((row.errored / row.total) * 100).toFixed(1);
      const isHighError = row.errored / row.total > 0.05;
      return (
        <Stack direction="horizontal" justify="end">
          <Badge variant={isHighError ? 'error' : 'healthy'} size="sm">
            {errorRate}%
          </Badge>
        </Stack>
      );
    },
  },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function RequestsTable() {
  // Sensible default: weekly granularity, 30-day period for requests
  const [granularity, setGranularity] = useState<Granularity>('weekly');
  const [period, setPeriod] = useState<Period>('30d');

  const rows = buildRows(DAILY_REQUESTS, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Requests over time</CardTitle>
            <CardDescription>Total, successful, and errored API calls per period</CardDescription>
          </Stack>
          <TableControls
            granularity={granularity}
            period={period}
            onGranularityChange={setGranularity}
            onPeriodChange={setPeriod}
          />
        </Stack>
      </CardHeader>
      <CardContent flush>
        <DataTable
          data={rows}
          columns={COLUMNS}
          getRowKey={(row) => row.period}
          emptyMessage="No data for selected period."
        />
      </CardContent>
    </Card>
  );
}
