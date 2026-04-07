'use client';

import { useState } from 'react';
import {
  Badge,
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
                    Successful
                  </Stack>
                </TableHead>
                <TableHead>
                  <Stack direction="horizontal" justify="end">
                    Errored
                  </Stack>
                </TableHead>
                <TableHead>
                  <Stack direction="horizontal" justify="end">
                    Error rate
                  </Stack>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5}>
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
                        <Stack direction="horizontal" justify="end">
                          <Text size="supporting" mono tabular weight="emphasis">
                            {fmtNum(row.total)}
                          </Text>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" justify="end">
                          <Text size="supporting" mono tabular color="status-healthy">
                            {fmtNum(row.successful)}
                          </Text>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" justify="end">
                          <Text
                            size="supporting"
                            mono
                            tabular
                            color={row.errored > 0 ? 'status-error' : 'muted'}
                          >
                            {fmtNum(row.errored)}
                          </Text>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" justify="end">
                          <Badge variant={isHighError ? 'error' : 'healthy'} size="sm">
                            {errorRate}%
                          </Badge>
                        </Stack>
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
