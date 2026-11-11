'use client';

/** @tier 3 — agents-scoped. The runs in flight right now, with the mix of states in the header. */

import { useRouter } from 'next/navigation';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableRoot,
  TableRow,
  Text,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { RUNNING_ROWS } from '../_lib/spend';
import { formatDuration } from '../_types';
import type { RunRow } from '../_types';
import { RunListRow } from './run-row';

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
  /** Why the runs could not be read; shown instead of the empty message. */
  error?: string | null;
}

export function RunningNowCard({ runs, counts, error = null }: RunningNowCardProps) {
  const total = counts?.total ?? runs.length;
  const router = useRouter();

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
              {error ?? 'Nothing is running. Start a run to see it here.'}
            </Text>
          </CardContent>
        ) : (
          // No header row: each row explains itself, and the whole row opens the run.
          <TableRoot>
            <Table>
              <TableBody>
                {runs.slice(0, RUNNING_ROWS).map((run) => {
                  const href = APP_ROUTES.agents.run(run.agentTypeSlug, run.id);
                  return (
                    <TableRow
                      key={run.id}
                      tabIndex={0}
                      aria-label={`Open run ${run.id.slice(0, 8)}`}
                      onClick={() => router.push(href)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' && e.target === e.currentTarget) router.push(href);
                      }}
                      style={{ cursor: 'pointer' }}
                    >
                      <TableCell>
                        <RunListRow row={run} showAgent time="elapsed" maxChips={1} />
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
