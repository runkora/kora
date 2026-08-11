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
import { DAILY_TOKENS, type DailyTokenRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

// ── Helpers ───────────────────────────────────────────────────────────────────

interface TokenRow {
  period: string;
  promptTokens: number;
  outputTokens: number;
  totalTokens: number;
}

function buildRows(data: DailyTokenRow[], granularity: Granularity, period: Period): TokenRow[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      promptTokens: r.promptTokens,
      outputTokens: r.outputTokens,
      totalTokens: r.totalTokens,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      promptTokens: rows.reduce((s, r) => s + r.promptTokens, 0),
      outputTokens: rows.reduce((s, r) => s + r.outputTokens, 0),
      totalTokens: rows.reduce((s, r) => s + r.totalTokens, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    promptTokens: rows.reduce((s, r) => s + r.promptTokens, 0),
    outputTokens: rows.reduce((s, r) => s + r.outputTokens, 0),
    totalTokens: rows.reduce((s, r) => s + r.totalTokens, 0),
  }));
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

const COLUMNS: DataTableColumn<TokenRow>[] = [
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
        {fmtTokens(row.totalTokens)}
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
        {fmtTokens(row.promptTokens)}
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
        {fmtTokens(row.outputTokens)}
      </Text>
    ),
  },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function TokensTable() {
  const [granularity, setGranularity] = useState<Granularity>('daily');
  const [period, setPeriod] = useState<Period>('30d');

  const rows = buildRows(DAILY_TOKENS, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Tokens over time</CardTitle>
            <CardDescription>Prompt and output token consumption per period</CardDescription>
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
