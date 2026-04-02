'use client';

import { useState } from 'react';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
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
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>Spend over time</CardTitle>
            <CardDescription>USD cost broken down by prompt and output tokens</CardDescription>
          </div>
          <TableControls
            granularity={granularity}
            period={period}
            onGranularityChange={setGranularity}
            onPeriodChange={setPeriod}
            defaultGranularity="daily"
            defaultPeriod="30d"
          />
        </div>
      </CardHeader>
      <CardContent flush>
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Period</TableHead>
                <TableHead>
                  <div className="flex justify-end">Total</div>
                </TableHead>
                <TableHead>
                  <div className="flex justify-end">Prompt</div>
                </TableHead>
                <TableHead>
                  <div className="flex justify-end">Output</div>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4}>
                    <div className="text-center py-8">
                      <Text size="supporting" color="muted">
                        No data for selected period
                      </Text>
                    </div>
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
                      <div className="flex justify-end">
                        <Text size="supporting" mono tabular weight="emphasis">
                          {fmt(row.totalUsd)}
                        </Text>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmt(row.promptUsd)}
                        </Text>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmt(row.outputUsd)}
                        </Text>
                      </div>
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
