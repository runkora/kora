'use client';

import { useState } from 'react';
import {
  Badge,
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
import { DAILY_REQUESTS, type DailyRequestRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

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

function fmtNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

// ── Component ─────────────────────────────────────────────────────────────────

export function RequestsTable() {
  // Sensible default: weekly granularity, 30-day period for requests
  const [granularity, setGranularity] = useState<Granularity>('weekly');
  const [period, setPeriod] = useState<Period>('30d');

  const rows = buildRows(DAILY_REQUESTS, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>Requests over time</CardTitle>
            <CardDescription>Total, successful, and errored API calls per period</CardDescription>
          </div>
          <TableControls
            granularity={granularity}
            period={period}
            onGranularityChange={setGranularity}
            onPeriodChange={setPeriod}
            defaultGranularity="weekly"
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
                  <div className="flex justify-end">Successful</div>
                </TableHead>
                <TableHead>
                  <div className="flex justify-end">Errored</div>
                </TableHead>
                <TableHead>
                  <div className="flex justify-end">Error rate</div>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5}>
                    <div className="text-center py-8">
                      <Text size="supporting" color="muted">
                        No data for selected period
                      </Text>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => {
                  const errorRate = ((row.errored / row.total) * 100).toFixed(1);
                  const isHighError = row.errored / row.total > 0.05;
                  return (
                    <TableRow key={row.period}>
                      <TableCell>
                        <Text size="supporting" mono>
                          {row.period}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end">
                          <Text size="supporting" mono tabular weight="emphasis">
                            {fmtNum(row.total)}
                          </Text>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end">
                          <Text size="supporting" mono tabular color="status-healthy">
                            {fmtNum(row.successful)}
                          </Text>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end">
                          <Text
                            size="supporting"
                            mono
                            tabular
                            color={row.errored > 0 ? 'status-error' : 'muted'}
                          >
                            {fmtNum(row.errored)}
                          </Text>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end">
                          <Badge variant={isHighError ? 'error' : 'healthy'} size="sm">
                            {errorRate}%
                          </Badge>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </TableRoot>
      </CardContent>
    </Card>
  );
}
