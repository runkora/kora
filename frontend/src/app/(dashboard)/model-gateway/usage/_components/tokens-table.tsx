'use client';

import { useState } from 'react';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
} from '@orbit/terra';
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
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Period</TableHead>
                <TableHead>
                  <Stack direction="horizontal" justify="end">
                    Total
                  </Stack>
                </TableHead>
                <TableHead>
                  <Stack direction="horizontal" justify="end">
                    Prompt
                  </Stack>
                </TableHead>
                <TableHead>
                  <Stack direction="horizontal" justify="end">
                    Output
                  </Stack>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableEmpty colSpan={4} message="No data for selected period." />
              ) : (
                rows.map((row) => (
                  <TableRow key={row.period}>
                    <TableCell>
                      <Text size="supporting" mono>
                        {row.period}
                      </Text>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" justify="end">
                        <Text size="supporting" mono tabular weight="emphasis">
                          {fmtTokens(row.totalTokens)}
                        </Text>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" justify="end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmtTokens(row.promptTokens)}
                        </Text>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" justify="end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmtTokens(row.outputTokens)}
                        </Text>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableRoot>
      </CardContent>
    </Card>
  );
}
