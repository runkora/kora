'use client';

/**
 * @tier 3 — runs-scoped. The Runs tab: every run the viewer can see as one line, newest first,
 * filtered on the server and kept live by the run stream.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  DataTable,
  Heading,
  Icon,
  InlineSelect,
  Skeleton,
  Stack,
  Text,
  type DataTableColumn,
} from '@runkora/hades';
import { FilterChip, RunSourceChips, RunStateIcon, StarterMark } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { fetchAgentTypes } from '../../agents/_services/agents-api';
import { shortRelativeTime } from '../../agents/_components/run-row';
import { indexAgentTypes, toRunRow, type AgentTypeRow, type RunRow } from '../../agents/_types';
import { useRunsFeed } from '../_hooks/use-runs-feed';
import {
  DEFAULT_RUN_FILTERS,
  QUICK_FILTERS,
  RUN_SOURCES,
  type RunFilters,
  type RunSourceFilter,
} from '../_types';

/** How long typing pauses before the search goes to the server. */
const SEARCH_DEBOUNCE_MS = 300;

const ANY = 'any';

/** The row's title: the task summary, or what is known before or without one. */
export function runTitle(r: Pick<RunRow, 'summary' | 'state'>): { text: string; known: boolean } {
  if (r.summary) return { text: r.summary.replace(/\.$/, ''), known: true };
  if (r.state === 'pending' || r.state === 'starting')
    return { text: 'Waiting to start', known: true };
  return { text: 'No task summary', known: false };
}

const titleOf = (r: RunRow) => runTitle(r).text;

/** The title in the row's emphasis, or muted when the run has no summary to show. */
function RunTitle({ run }: { run: RunRow }) {
  const title = runTitle(run);
  return title.known ? (
    <Text size="supporting" weight="emphasis" truncate>
      {title.text}
    </Text>
  ) : (
    <Text size="supporting" color="subtle" truncate>
      {title.text}
    </Text>
  );
}

/** The two states a person acts on, spelled out: waiting for a message, or stopped at its budget. */
function StateMarker({ run }: { run: RunRow }) {
  const budget = run.state === 'budget_exhausted';
  if (!budget && run.state !== 'idle') return null;
  const color = budget ? 'status-error' : 'status-degraded';
  return (
    <Stack direction="horizontal" align="center" gap="tight">
      <Icon name={budget ? 'dollar-sign' : 'pause'} size="2xs" color={color} label="" shrink />
      <Text size="caption" color={color}>
        {budget ? 'Stopped at its budget' : 'Waiting'}
      </Text>
    </Stack>
  );
}

/** One line: state, title and agent, then what it waits on, what it is about and who started it. */
function RunLine({ run }: { run: RunRow }) {
  return (
    <Stack direction="horizontal" align="center" gap="cozy" grow minWidth="none">
      <RunStateIcon state={run.state} />
      <Stack direction="horizontal" align="center" gap="snug" grow minWidth="none">
        <RunTitle run={run} />
        <Stack>
          <Badge variant="secondary" size="sm">
            <Icon name="bot" size="2xs" label="" />
            {run.agentTypeName}
          </Badge>
        </Stack>
      </Stack>
      <StateMarker run={run} />
      <Stack direction="horizontal" align="center" gap="cozy">
        <RunSourceChips sources={run.sources} max={1} />
        <StarterMark row={run} />
      </Stack>
    </Stack>
  );
}

const COLUMNS: DataTableColumn<RunRow>[] = [
  {
    id: 'run',
    header: 'Run',
    gridWidth: 'minmax(0, 1fr)',
    minWidth: '24rem',
    cell: (r) => <RunLine run={r} />,
  },
  {
    // Its own fixed track keeps the starter marks lined up down the list whatever the time's width.
    id: 'when',
    header: 'Started',
    gridWidth: '2.5rem',
    align: 'right',
    cell: (r) => (
      <Text size="caption" color="muted" tabular>
        {r.startedAt ? shortRelativeTime(r.startedAt) : 'now'}
      </Text>
    ),
  },
];

function RunsLoading() {
  return (
    <Stack gap="cozy" role="status" aria-label="Loading runs" aria-busy="true">
      {Array.from({ length: 6 }, (_, i) => (
        <Skeleton key={i} shape="block" width="full" size="lg" />
      ))}
    </Stack>
  );
}

export function RunsPage() {
  const router = useRouter();
  const [filters, setFilters] = useState<RunFilters>(DEFAULT_RUN_FILTERS);
  const [search, setSearch] = useState('');
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>([]);
  const feed = useRunsFeed(filters);

  useEffect(() => {
    const timer = window.setTimeout(
      () => setFilters((f) => (f.query === search ? f : { ...f, query: search })),
      SEARCH_DEBOUNCE_MS,
    );
    return () => window.clearTimeout(timer);
  }, [search]);

  // The Agent select lists the agents the viewer can read; the runs carry their own names.
  useEffect(() => {
    void fetchAgentTypes().then(({ data }) => setAgentTypes(data));
  }, []);

  // A new page starts at its top, where the new runs bar shows.
  const top = useRef<HTMLDivElement>(null);
  useEffect(() => {
    top.current?.scrollIntoView?.({ block: 'start' });
  }, [feed.pageIndex]);

  const rows = useMemo(() => {
    const byId = indexAgentTypes(agentTypes);
    return feed.runs.map((r) => toRunRow(r, byId));
  }, [feed.runs, agentTypes]);

  const filtered =
    filters.quick !== 'all' ||
    filters.agentTypeId !== null ||
    filters.source !== null ||
    filters.query !== '';
  const showPager = feed.hasNewer || feed.hasOlder;

  return (
    <Stack gap="relaxed" ref={top}>
      <Stack direction="horizontal" align="center" justify="between" gap="relaxed">
        <Stack gap="tight">
          <Heading variant="title">Runs</Heading>
          <Text size="supporting" color="muted">
            Every run you can see, from you, your team and your triggers.
          </Text>
        </Stack>
        <Button size="sm" asChild>
          <Link href={APP_ROUTES.chat}>
            <Icon name="play" size="xs" />
            Start work
          </Link>
        </Button>
      </Stack>
      {feed.error !== null && rows.length > 0 && (
        <Stack direction="horizontal" align="center" justify="center" gap="snug">
          <Text size="caption" color="status-error">
            {feed.error}
          </Text>
          <Button variant="outline" size="sm" onClick={feed.retry}>
            Try again
          </Button>
        </Stack>
      )}
      {feed.freshCount > 0 && (
        <Stack align="center">
          <Button variant="secondary" size="sm" onClick={feed.showFresh}>
            <Icon name="arrow-up" size="xs" />
            {feed.freshCount === 1 ? '1 new run' : `${feed.freshCount} new runs`}
          </Button>
        </Stack>
      )}
      <DataTable
        data={rows}
        columns={COLUMNS}
        getRowKey={(r) => r.id}
        onRowClick={(r) => router.push(APP_ROUTES.agents.run(r.agentTypeSlug, r.id))}
        rowLabel={(r) => `Open run ${titleOf(r)}`}
        noun="runs"
        hideHeader
        columnToggle={false}
        searchPlaceholder="Search runs…"
        searchQuery={search}
        onSearchChange={setSearch}
        bodyState={feed.loading ? <RunsLoading /> : undefined}
        error={
          feed.error !== null && rows.length === 0 ? (
            <Stack align="center" gap="snug" paddingBlock="comfortable">
              <Text size="supporting" color="muted">
                {feed.error}
              </Text>
              <Button variant="outline" size="sm" onClick={feed.retry}>
                Try again
              </Button>
            </Stack>
          ) : undefined
        }
        quickFilters={
          <Stack direction="horizontal" gap="snug" wrap>
            {QUICK_FILTERS.map((q) => (
              <FilterChip
                key={q.value}
                label={q.label}
                count={null}
                active={filters.quick === q.value}
                onClick={() => setFilters((f) => ({ ...f, quick: q.value }))}
              />
            ))}
          </Stack>
        }
        toolbarActions={
          <Stack direction="horizontal" align="center" gap="snug">
            <Text size="supporting" color="muted">
              Agent
            </Text>
            <InlineSelect
              aria-label="Agent"
              value={filters.agentTypeId ?? ANY}
              onValueChange={(v) =>
                setFilters((f) => ({ ...f, agentTypeId: v === ANY ? null : v }))
              }
              options={[
                { value: ANY, label: ANY },
                ...agentTypes.map((a) => ({ value: a.id, label: a.name })),
              ]}
            />
            <Text size="supporting" color="muted">
              Source
            </Text>
            <InlineSelect
              aria-label="Source"
              value={filters.source ?? ANY}
              onValueChange={(v) =>
                setFilters((f) => ({ ...f, source: v === ANY ? null : (v as RunSourceFilter) }))
              }
              options={[{ value: ANY, label: ANY }, ...RUN_SOURCES]}
            />
          </Stack>
        }
        emptyMessage={
          filtered
            ? 'No runs match these filters.'
            : 'No runs yet. Runs from you, your team and your triggers show here.'
        }
        filteredEmptyMessage="No runs match these filters."
      />
      {showPager && (
        <Stack direction="horizontal" align="center" justify="end" gap="snug">
          {feed.pageError !== null && (
            <Text size="caption" color="status-error">
              {feed.pageError}
            </Text>
          )}
          <Text size="caption" color="muted" tabular>
            Page {feed.pageIndex + 1}
          </Text>
          <Button
            variant="outline"
            size="sm"
            onClick={feed.newer}
            disabled={!feed.hasNewer || feed.pageLoading}
          >
            <Icon name="chevron-left" size="xs" />
            Newer
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={feed.older}
            disabled={!feed.hasOlder || feed.pageLoading}
          >
            {feed.pageLoading ? 'Loading…' : 'Older'}
            <Icon name="chevron-right" size="xs" />
          </Button>
        </Stack>
      )}
    </Stack>
  );
}
