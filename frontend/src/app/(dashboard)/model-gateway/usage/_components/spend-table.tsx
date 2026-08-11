'use client';

import { useState } from 'react';
import {
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
import { DAILY_SPEND, type DailySpendRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';
import { fmtCost } from '@orbit/utils';

// ── Types & helpers ───────────────────────────────────────────────────────────

interface SpendRow {
  period: string;
  totalUsd: number;
  promptUsd: number;
  outputUsd: number;
}

function buildRows(data: DailySpendRow[], granularity: Granularity, period: Period): SpendRow[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      totalUsd: r.totalUsd,
      promptUsd: r.promptUsd,
      outputUsd: r.outputUsd,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      totalUsd: rows.reduce((s, r) => s + r.totalUsd, 0),
      promptUsd: rows.reduce((s, r) => s + r.promptUsd, 0),
      outputUsd: rows.reduce((s, r) => s + r.outputUsd, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    totalUsd: rows.reduce((s, r) => s + r.totalUsd, 0),
    promptUsd: rows.reduce((s, r) => s + r.promptUsd, 0),
    outputUsd: rows.reduce((s, r) => s + r.outputUsd, 0),
  }));
}

const COLUMNS: DataTableColumn<SpendRow>[] = [
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
    gridWidth: '120px',
    align: 'right',
    cell: (row) => (
      <Text size="supporting" mono tabular weight="emphasis" align="end">
        {fmtCost(row.totalUsd)}
      </Text>
    ),
  },
  {
    id: 'prompt',
    header: 'Prompt',
    gridWidth: '120px',
    align: 'right',
    cell: (row) => (
      <Text size="supporting" mono tabular color="muted" align="end">
        {fmtCost(row.promptUsd)}
      </Text>
    ),
  },
  {
    id: 'output',
    header: 'Output',
    gridWidth: '120px',
    align: 'right',
    cell: (row) => (
      <Text size="supporting" mono tabular color="muted" align="end">
        {fmtCost(row.outputUsd)}
      </Text>
    ),
  },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function SpendTable() {
  const [granularity, setGranularity] = useState<Granularity>('daily');
  const [period, setPeriod] = useState<Period>('30d');

  const rows = buildRows(DAILY_SPEND, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Spend over time</CardTitle>
            <CardDescription>USD cost broken down by prompt and output tokens</CardDescription>
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
