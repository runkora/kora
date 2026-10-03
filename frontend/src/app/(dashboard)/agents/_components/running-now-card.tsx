'use client';

/** @tier 3 — agents-scoped. The runs in flight right now, with the mix of states in the header. */

import Link from 'next/link';
import {
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Stack,
  StatusDot,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
} from '@orbit/terra';
import type { StatusDotProps } from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import { formatDuration, formatRelativeTime } from '../_types';
import type { RunRow } from '../_types';

/** How each live run state reads in the table. */
export const LIVE_STATES: Record<
  string,
  { status: NonNullable<StatusDotProps['status']>; label: string }
> = {
  active: { status: 'healthy', label: 'Running' },
  completing: { status: 'healthy', label: 'Finishing' },
  starting: { status: 'pending', label: 'Starting' },
  pending: { status: 'pending', label: 'Queued' },
};

export function isLiveRun(run: RunRow): boolean {
  return run.state in LIVE_STATES;
}

/** The most runs the table lists. The header counts them all. */
const MAX_ROWS = 10;

export interface RunningNowCardProps {
  /** Only runs that are live; see `isLiveRun`. */
  runs: RunRow[];
}

export function RunningNowCard({ runs }: RunningNowCardProps) {
  const count = (...states: string[]) => runs.filter((r) => states.includes(r.state)).length;
  const longest = Math.max(0, ...runs.map((r) => r.durationMs ?? 0));

  return (
    <Stack asChild overflow="hidden">
      <Card column divide>
        <CardHeader
          leadingIcon="activity"
          title="Running now"
          description={
            runs.length > 0
              ? `${runs.length} ${runs.length === 1 ? 'run' : 'runs'} in progress`
              : undefined
          }
          action={
            runs.length > 0 ? (
              <Stack gap="tight" align="end">
                <Text size="caption" color="muted">
                  {count('active', 'completing')} running · {count('starting')} starting ·{' '}
                  {count('pending')} queued
                </Text>
                <Text size="caption" color="subtle">
                  Longest {formatDuration(longest)}
                </Text>
              </Stack>
            ) : undefined
          }
          divider
        />
        {runs.length === 0 ? (
          <CardContent padded grow>
            <Text size="supporting" color="muted">
              Nothing is running. Start a run to see it here.
            </Text>
          </CardContent>
        ) : (
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Agent</TableHead>
                  <TableHead>Run</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Started</TableHead>
                  <TableHead align="right">Elapsed</TableHead>
                  <TableHead align="right">Tool calls</TableHead>
                  <TableHead align="right" srOnly>
                    Actions
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.slice(0, MAX_ROWS).map((run) => {
                  const state = LIVE_STATES[run.state];
                  return (
                    <TableRow key={run.id}>
                      <TableCell>
                        <Text size="supporting" weight="emphasis">
                          {run.agentTypeName}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Text mono size="caption" color="muted">
                          {run.id}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" align="center" gap="snug">
                          <StatusDot
                            status={state?.status ?? 'idle'}
                            glow={state?.status === 'healthy'}
                            pulse={state?.status === 'pending'}
                          />
                          <Text size="supporting">{state?.label}</Text>
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Text size="supporting" color="muted">
                          {formatRelativeTime(run.startedAt ?? run.createdAt)}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Text mono tabular size="supporting" align="end">
                          {formatDuration(run.durationMs)}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Text mono tabular size="supporting" align="end">
                          {run.toolCalls ?? '—'}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" justify="end">
                          <Button variant="outline" size="sm" asChild>
                            <Link href={APP_ROUTES.agents.run(run.agentTypeSlug, run.id)}>
                              Open
                            </Link>
                          </Button>
                        </Stack>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableRoot>
        )}
        {runs.length > MAX_ROWS && (
          <CardFooter compact>
            <Text size="caption" color="subtle">
              +{runs.length - MAX_ROWS} more {runs.length - MAX_ROWS === 1 ? 'run' : 'runs'} not
              shown
            </Text>
          </CardFooter>
        )}
      </Card>
    </Stack>
  );
}
