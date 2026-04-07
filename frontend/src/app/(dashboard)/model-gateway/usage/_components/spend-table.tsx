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
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
} from '@orbit/terra';
import { DAILY_SPEND, type DailySpendRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

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

function fmt(n: number) {
  return `$${n.toFixed(4)}`;
}

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
                <TableRow>
                  <TableCell colSpan={4}>
                    <Stack align="center" justify="center">
                      <Stack asChild>
                        <figure className="py-8 text-center m-0">
                          <Text size="supporting" color="muted">
                            No data for selected period
                          </Text>
                        </figure>
                      </Stack>
                    </Stack>
                  </TableCell>
                </TableRow>
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
                          {fmt(row.totalUsd)}
                        </Text>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" justify="end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmt(row.promptUsd)}
                        </Text>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" justify="end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmt(row.outputUsd)}
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
