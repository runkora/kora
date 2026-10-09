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
} from '@orbit/hades';
import type { StatusDotProps } from '@orbit/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import { RUNNING_ROWS } from '../_lib/spend';
import { formatDuration, formatRelativeTime } from '../_types';
import type { RunRow } from '../_types';

/** How each live run state reads in the table. */
const LIVE_STATES: Record<
  string,
  { status: NonNullable<StatusDotProps['status']>; label: string }
> = {
  active: { status: 'healthy', label: 'Running' },
  completing: { status: 'healthy', label: 'Finishing' },
  starting: { status: 'pending', label: 'Starting' },
  pending: { status: 'pending', label: 'Queued' },
};

/** Every run state's dot and label: the live states above, then the finished ones. */
const RUN_STATES: Record<string, { status: NonNullable<StatusDotProps['status']>; label: string }> =
  {
    ...LIVE_STATES,
    idle: { status: 'degraded', label: 'Waiting' },
    completed: { status: 'idle', label: 'Completed' },
    failed: { status: 'error', label: 'Failed' },
    budget_exhausted: { status: 'error', label: 'Budget exhausted' },
  };

export function RunStateDot({ state: name }: { state: string }) {
  const state = RUN_STATES[name];
  return (
    <Stack direction="horizontal" align="center" gap="snug">
      <StatusDot
        status={state?.status ?? 'idle'}
        glow={state?.status === 'healthy'}
        pulse={state?.status === 'pending'}
      />
      <Text size="supporting">{state?.label ?? name}</Text>
    </Stack>
  );
}

export interface RunningNowCardProps {
  /** The newest live runs, at most RUNNING_ROWS of them. */
  runs: RunRow[];
  /** Every live run, counted by the server; null when the counts could not be read. */
  counts: {
    running: number;
    starting: number;
    queued: number;
    total: number;
    longestMs: number;
  } | null;
}

export function RunningNowCard({ runs, counts }: RunningNowCardProps) {
  const total = counts?.total ?? runs.length;

  return (
    <Stack asChild overflow="hidden">
      <Card column divide>
        <CardHeader
          leadingIcon="activity"
          title="Running now"
          description={
            total > 0 ? `${total} ${total === 1 ? 'run' : 'runs'} in progress` : undefined
          }
          action={
            counts && total > 0 ? (
              <Stack gap="tight" align="end">
                <Text size="caption" color="muted">
                  {counts.running} running · {counts.starting} starting · {counts.queued} queued
                </Text>
                <Text size="caption" color="subtle">
                  Longest {formatDuration(counts.longestMs)}
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
                {runs.slice(0, RUNNING_ROWS).map((run) => {
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
                        <RunStateDot state={run.state} />
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
        {total > runs.length && (
          <CardFooter compact>
            <Text size="caption" color="subtle">
              +{total - runs.length} more {total - runs.length === 1 ? 'run' : 'runs'} not shown
            </Text>
          </CardFooter>
        )}
      </Card>
    </Stack>
  );
}
