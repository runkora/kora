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
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>Tokens over time</CardTitle>
            <CardDescription>Prompt and output token consumption per period</CardDescription>
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
                          {fmtTokens(row.totalTokens)}
                        </Text>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmtTokens(row.promptTokens)}
                        </Text>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end">
                        <Text size="supporting" mono tabular color="muted">
                          {fmtTokens(row.outputTokens)}
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
