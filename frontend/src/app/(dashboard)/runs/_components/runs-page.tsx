'use client';

/**
 * @tier 3 — runs-scoped. The Runs tab: every run the viewer can see as one line, newest first,
 * filtered on the server and kept live by the run stream.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Badge,
  Button,
  Callout,
  DataTable,
  EmptyState,
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
import {
  formatRelativeTime,
  indexAgentTypes,
  runTitle,
  shortRelativeTime,
  toRunRow,
  type AgentTypeRow,
  type RunRow,
} from '../../agents/_types';
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
  return (
    <Text size="caption" color={budget ? 'status-error' : 'status-degraded'}>
      {budget ? 'Stopped at its budget' : 'Waiting'}
    </Text>
  );
}

/** A run row, marked fresh when it was created while the page was open. */
type Row = RunRow & { fresh: boolean };

/** One line: state, title and agent, then what it waits on, what it is about and who started it. */
function RunLine({ run }: { run: Row }) {
  return (
    <Stack direction="horizontal" align="center" gap="cozy" grow minWidth="none">
      <RunStateIcon state={run.state} />
      <Stack direction="horizontal" align="center" gap="snug" grow minWidth="none">
        <RunTitle run={run} />
        {/* The TruncatingBadge pattern, keeping the bot icon: a long agent name ends in an ellipsis. */}
        <Stack align="stretch" minWidth="none" overflow="hidden">
          <Badge variant="secondary" size="sm" title={run.agentTypeName}>
            <Icon name="bot" size="2xs" label="" />
            <Text as="span" size="caption" truncate>
              {run.agentTypeName}
            </Text>
          </Badge>
        </Stack>
        {run.fresh && (
          <Badge variant="default" size="sm">
            New
          </Badge>
        )}
      </Stack>
      <StateMarker run={run} />
      <Stack direction="horizontal" align="center" gap="cozy">
        <RunSourceChips sources={run.sources} max={1} />
        <StarterMark row={run} />
      </Stack>
    </Stack>
  );
}

const COLUMNS: DataTableColumn<Row>[] = [
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
      <Text
        size="caption"
        color="muted"
        tabular
        title={formatRelativeTime(r.startedAt ?? r.createdAt)}
      >
        {shortRelativeTime(r.startedAt ?? r.createdAt)}
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

/** No rows: the first page failed, the filters match nothing, or no run exists yet. */
function RunsEmpty({
  error,
  onRetry,
  filtered,
  onClearFilters,
}: {
  error: string | null;
  onRetry: () => void;
  filtered: boolean;
  onClearFilters: () => void;
}) {
  if (error !== null)
    return (
      <EmptyState
        icon="triangle-alert"
        title="Could not load runs"
        description={error}
        action={
          <Button size="xs" variant="secondary" onClick={onRetry}>
            <Icon name="refresh-cw" size="2xs" />
            Try again
          </Button>
        }
      />
    );
  if (filtered)
    return (
      <EmptyState
        icon="search-x"
        title="No runs match these filters"
        description="New runs that match will appear here."
        action={
          <Button size="xs" variant="secondary" onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      />
    );
  return (
    <EmptyState
      icon="play-circle"
      title="No runs yet"
      description="Runs from you, your team and your triggers show here."
    />
  );
}

export function RunsPage() {
  const router = useRouter();
  const [filters, setFilters] = useState<RunFilters>(DEFAULT_RUN_FILTERS);
  const [search, setSearch] = useState('');
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>([]);
  const [agentsError, setAgentsError] = useState(false);
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
    void fetchAgentTypes().then(({ data, error }) => {
      setAgentTypes(data);
      setAgentsError(error !== null);
    });
  }, []);

  // A new page starts at its top, where the new runs bar shows.
  const top = useRef<HTMLDivElement>(null);
  useEffect(() => {
    top.current?.scrollIntoView?.({ block: 'start' });
  }, [feed.pageIndex]);

  const rows = useMemo(() => {
    const byId = indexAgentTypes(agentTypes);
    return feed.runs.map((r) => ({ ...toRunRow(r, byId), fresh: feed.liveIds.has(r.id) }));
  }, [feed.runs, feed.liveIds, agentTypes]);

  const filtered =
    filters.quick !== 'all' ||
    filters.agentTypeId !== null ||
    filters.source !== null ||
    filters.query !== '';
  const showPager = feed.hasNewer || feed.hasOlder;

  return (
    <Stack gap="comfortable" ref={top}>
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
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Stack grow>
            <Text size="caption" color="status-error">
              {feed.error}
            </Text>
          </Stack>
          <Button variant="outline" size="xs" onClick={feed.retry}>
            Try again
          </Button>
        </Alert>
      )}
      {feed.reconnecting && (
        <Callout variant="warning">
          <Text size="caption">
            The live stream dropped and is reconnecting. New runs and changes appear once it is
            back.
          </Text>
        </Callout>
      )}
      <Stack gap="cozy">
        {feed.freshCount > 0 && (
          <Stack direction="horizontal" justify="end">
            <Button size="xs" variant="accent-outline" shape="pill" onClick={feed.showFresh}>
              <Icon name="arrow-up" size="2xs" />
              {feed.freshCount} new {feed.freshCount === 1 ? 'run' : 'runs'}
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
          bodyState={
            feed.loading ? (
              <RunsLoading />
            ) : rows.length === 0 ? (
              <RunsEmpty
                error={feed.error}
                onRetry={feed.retry}
                filtered={filtered}
                onClearFilters={() => {
                  setSearch('');
                  setFilters(DEFAULT_RUN_FILTERS);
                }}
              />
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
              {agentsError ? (
                <Text size="caption" color="muted">
                  Could not load agents.
                </Text>
              ) : (
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
              )}
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
        />
        {showPager && (
          <Stack direction="horizontal" align="center" gap="tight" justify="end">
            {feed.pageError !== null && (
              <Text size="caption" color="status-error">
                {feed.pageError}
              </Text>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={feed.newer}
              disabled={!feed.hasNewer || feed.pageLoading}
            >
              Newer
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={feed.older}
              disabled={!feed.hasOlder || feed.pageLoading}
            >
              Older
            </Button>
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}
