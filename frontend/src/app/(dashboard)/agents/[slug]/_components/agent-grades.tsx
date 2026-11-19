'use client';

/**
 * @tier 3 · agents/[slug]-scoped. The Grades tab. `GradersList` shows each Grader's pass rate over
 * its last `WINDOW` graded runs against its `minPassRate`, failing first. `GraderPage` shows one Grader's
 * questions and their failed or uncertain runs. Every string is a stored field or a count of them.
 */

import { useState } from 'react';
import NextLink from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Anchor,
  Badge,
  Button,
  DataTable,
  EmptyState,
  Heading,
  Icon,
  List,
  ListItem,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SegmentedControl,
  Stack,
  Text,
  type DataTableColumn,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing/routes';
import type {
  GraderDetail,
  GraderFlaggedRun,
  GraderQuestion,
  GraderStatus,
  GraderSummary,
} from '@runkora/shared/api';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@runkora/lib/pagination';
import {
  GRADER_STATUS_LABEL,
  VERDICT_BADGE,
  VERDICT_LABEL,
  WINDOW,
  gradeRunsHref,
  passRate,
  passesOn,
} from '../../_lib/grades';
import { useAgentGrader, useAgentGraders } from '../_hooks/use-agent-graders';
import { shortRelativeTime } from '../../_components/run-row';
import { formatRelativeTime } from '../../_types';

/** Runs listed before "Show all". */
const RUNS_SHOWN = 5;

const graderName = (g: GraderSummary) => g.description || g.name;

/** "52% passed · needs 90%"; the minimum only when the Grader sets one. */
function RateText({ g }: { g: GraderSummary }) {
  const rate = passRate(g.passed, g.graded);
  if (rate === null) return null;
  return (
    <Text size="supporting" tabular color={g.status === 'failing' ? 'status-error' : 'foreground'}>
      {rate}% passed
      {g.min_pass_rate !== null && (
        <Text as="span" size="supporting" color="subtle">
          {' '}
          · needs {g.min_pass_rate}%
        </Text>
      )}
    </Text>
  );
}

// ── Runs ─────────────────────────────────────────────────────────────────────

/** The agent's Runs tab filtered to one question's failed runs. */
function AllFailedLink({
  slug,
  grader,
  question,
}: {
  slug: string;
  grader: string;
  question: string;
}) {
  return (
    <Stack direction="horizontal">
      <Anchor asChild size="sm">
        <NextLink href={gradeRunsHref(slug, { grader, question, verdict: 'fail' })}>
          All failed runs for this question →
        </NextLink>
      </Anchor>
    </Stack>
  );
}

/** A question's failed or uncertain runs, and the link to all its failed runs when it has any. */
function QuestionRuns({ grader, q, slug }: { grader: string; q: GraderQuestion; slug: string }) {
  return (
    <Stack gap="snug">
      <RunList runs={q.runs} slug={slug} />
      {q.failed > 0 && <AllFailedLink slug={slug} grader={grader} question={q.name} />}
    </Stack>
  );
}

/** Runs newest first, each with its verdict; a row opens the run page. */
function RunList({ runs, slug }: { runs: GraderFlaggedRun[]; slug: string }) {
  const router = useRouter();
  const [all, setAll] = useState(false);
  const shown = all ? runs : runs.slice(0, RUNS_SHOWN);
  const columns: DataTableColumn<GraderFlaggedRun>[] = [
    {
      id: 'verdict',
      header: 'Verdict',
      gridWidth: '6.5rem',
      cell: (r) => (
        <Badge variant={VERDICT_BADGE[r.verdict]} size="sm">
          {VERDICT_LABEL[r.verdict]}
        </Badge>
      ),
    },
    {
      id: 'time',
      header: 'Started',
      gridWidth: '4rem',
      cell: (r) => (
        <Text size="supporting" color="subtle" tabular title={formatRelativeTime(r.started_at)}>
          {shortRelativeTime(r.started_at)}
        </Text>
      ),
    },
    {
      id: 'title',
      header: 'Run',
      gridWidth: 'minmax(0, 1fr)',
      cell: (r) => (
        <Text size="supporting" truncate>
          {r.title ?? r.run_id}
        </Text>
      ),
    },
    {
      id: 'open',
      header: '',
      gridWidth: '2rem',
      align: 'right',
      cell: () => <Icon name="chevron-right" size="xs" color="subtle" />,
    },
  ];
  return (
    <DataTable
      data={shown}
      columns={columns}
      getRowKey={(r) => r.run_id}
      hideHeader
      columnToggle={false}
      noun="runs"
      toolbarTitle={
        <Text size="supporting" weight="emphasis" tabular>
          Failed or uncertain runs ({runs.length})
        </Text>
      }
      toolbarActions={
        runs.length > RUNS_SHOWN ? (
          <Button variant="ghost" size="sm" onClick={() => setAll((a) => !a)}>
            {all ? 'Show fewer' : `Show all ${runs.length}`}
          </Button>
        ) : undefined
      }
      onRowClick={(r) => router.push(APP_ROUTES.agents.run(slug, r.run_id))}
      rowLabel={(r) => `Open run ${r.title ?? r.run_id}`}
    />
  );
}

// ── Every Grader ─────────────────────────────────────────────────────────────

const STATUS_ICON = {
  failing: { name: 'circle-x', color: 'status-error' },
  passing: { name: 'circle-check', color: 'status-healthy' },
  no_minimum: { name: 'minus', color: 'subtle' },
  not_enough_runs: { name: 'clock', color: 'subtle' },
} as const satisfies Record<GraderStatus, { name: string; color: string }>;

type Filter = 'all' | 'failing' | 'passing' | 'no_minimum';

const FILTER_LABEL: Record<Filter, string> = {
  all: 'All',
  failing: 'Failing',
  passing: 'Passing',
  no_minimum: 'No minimum',
};

const matches = (g: GraderSummary, q: string) =>
  g.name.toLowerCase().includes(q) || g.description.toLowerCase().includes(q);

const COLUMNS: DataTableColumn<GraderSummary>[] = [
  {
    id: 'grader',
    header: 'Grader',
    gridWidth: 'minmax(0, 1fr)',
    cell: (g) => {
      const icon = STATUS_ICON[g.status];
      return (
        <Stack direction="horizontal" align="start" gap="snug" minWidth="none">
          <Icon
            name={icon.name}
            size="sm"
            color={icon.color}
            label={GRADER_STATUS_LABEL[g.status]}
          />
          <Stack gap="tight" minWidth="none">
            <Text
              size="supporting"
              weight={g.status === 'failing' ? 'emphasis' : 'regular'}
              truncate
            >
              {graderName(g)}
            </Text>
            <Text size="caption" color="subtle" mono>
              {g.name}
            </Text>
          </Stack>
        </Stack>
      );
    },
  },
  {
    id: 'rate',
    header: 'Passed',
    gridWidth: '11rem',
    align: 'right',
    cell: (g) =>
      g.status === 'not_enough_runs' ? (
        <Text size="supporting" color="subtle" tabular>
          {GRADER_STATUS_LABEL.not_enough_runs}
        </Text>
      ) : (
        <RateText g={g} />
      ),
  },
  {
    id: 'runs',
    header: 'Graded runs',
    gridWidth: '7rem',
    align: 'right',
    cell: (g) => (
      <Text size="supporting" color="muted" tabular>
        {g.graded}
        {g.too_large > 0 && ` · ${g.too_large} too large`}
      </Text>
    ),
  },
];

/** The agent's Graders in the API's order, failing first. */
export function GradersList({
  graders,
  onOpen,
}: {
  graders: GraderSummary[];
  onOpen: (name: string) => void;
}) {
  const [filter, setFilter] = useState<Filter>('all');
  if (!graders.length)
    return (
      <EmptyState
        icon="file-check"
        title="No Graders"
        description="This agent has no Grader resources"
      />
    );

  const count = (f: Filter) =>
    f === 'all' ? graders.length : graders.filter((g) => g.status === f).length;
  const rows = filter === 'all' ? graders : graders.filter((g) => g.status === filter);

  return (
    <DataTable
      // A new filter starts again from page one.
      key={filter}
      data={rows}
      columns={COLUMNS}
      getRowKey={(g) => g.name}
      columnToggle={false}
      noun="Graders"
      onRowClick={(g) => onOpen(g.name)}
      rowLabel={(g) => `Open ${graderName(g)}`}
      searchFn={matches}
      searchPlaceholder="Search Graders…"
      emptyMessage="No Graders match this filter."
      quickFilters={
        <SegmentedControl
          aria-label="Grader status"
          value={filter}
          onChange={(v) => setFilter(v as Filter)}
          options={(Object.keys(FILTER_LABEL) as Filter[]).map((f) => ({
            value: f,
            label: FILTER_LABEL[f],
            count: count(f),
          }))}
        />
      }
      pagination={{ pageSizeOptions: PAGE_SIZES, defaultPageSize: DEFAULT_PAGE_SIZE }}
      toolbarTitle={
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis" tabular>
            {count('failing')} of {graders.length} Graders failing
          </Text>
          <Text size="caption" color="subtle" tabular>
            Last {WINDOW} graded runs per Grader
          </Text>
        </Stack>
      }
    />
  );
}

// ── One Grader ───────────────────────────────────────────────────────────────

/** The spec's fields: `appliesWhen`, each question with its pass condition, version and model. */
function SpecLink({ grader }: { grader: GraderDetail }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm">
          View spec
        </Button>
      </PopoverTrigger>
      <PopoverContent wide>
        <Stack gap="cozy">
          {grader.applies_when && (
            <Stack gap="tight">
              <Text size="caption" color="subtle">
                Applies when
              </Text>
              <Text size="supporting">{grader.applies_when}</Text>
            </Stack>
          )}
          <List gap="cozy">
            {grader.questions.map((q) => (
              <ListItem key={q.name}>
                <Stack gap="tight">
                  <Text size="caption" mono>
                    {q.name}
                  </Text>
                  <Text size="supporting" color="muted">
                    {q.question}
                  </Text>
                  <Text size="caption" color="subtle">
                    {passesOn(q)}
                  </Text>
                </Stack>
              </ListItem>
            ))}
          </List>
          <Text size="caption" color="subtle" tabular>
            Version {grader.version} · {grader.model}
          </Text>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

const PAGE_BADGE = {
  failing: 'error',
  passing: 'healthy',
  no_minimum: 'muted',
  not_enough_runs: 'muted',
} as const satisfies Record<GraderStatus, string>;

/** "Fail 5 · Uncertain 2", leaving out a zero. */
const counts = (q: GraderQuestion) =>
  [q.failed && `Fail ${q.failed}`, q.uncertain && `Uncertain ${q.uncertain}`]
    .filter(Boolean)
    .join(' · ');

/** One row per question with its pass condition and verdict counts; a row opens to its runs. */
function Questions({ grader, slug }: { grader: GraderDetail; slug: string }) {
  const rows = grader.questions;
  const single = rows.length === 1;
  const first = rows.find((q) => q.runs.length)?.name;
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set(first && !single ? [first] : []));
  const columns: DataTableColumn<GraderQuestion>[] = [
    {
      id: 'question',
      header: 'Question',
      gridWidth: 'minmax(0, 1fr)',
      wrap: true,
      cell: (q) => (
        <Stack gap="tight" minWidth="none">
          <Text size="supporting">{q.question}</Text>
          <Text size="caption" color="subtle">
            {passesOn(q)}
          </Text>
        </Stack>
      ),
    },
  ];
  if (!single)
    columns.push({
      id: 'counts',
      header: 'Verdicts',
      gridWidth: '13rem',
      align: 'right',
      cell: (q) => (
        <Stack direction="horizontal" align="center" justify="end" gap="snug">
          <Text size="supporting" tabular>
            {counts(q)}
          </Text>
          <Icon
            name={open.has(q.name) ? 'chevron-down' : 'chevron-right'}
            size="xs"
            color="subtle"
          />
        </Stack>
      ),
    });
  return (
    <DataTable
      data={rows}
      columns={columns}
      getRowKey={(q) => q.name}
      hideHeader
      columnToggle={false}
      noun="questions"
      expandedKeys={open}
      onExpandedChange={(key, expanded) => {
        if (single) return;
        setOpen((s) => {
          const next = new Set(s);
          if (expanded) next.add(key);
          else next.delete(key);
          return next;
        });
      }}
      renderExpanded={
        single
          ? undefined
          : (q) =>
              q.runs.length ? (
                <QuestionRuns grader={grader.name} q={q} slug={slug} />
              ) : (
                <Text size="supporting" color="subtle">
                  No failed or uncertain runs
                </Text>
              )
      }
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          What it asks
        </Text>
      }
      toolbarActions={<SpecLink grader={grader} />}
    />
  );
}

/** One Grader: its status, its questions and their failed or uncertain runs. */
export function GraderPage({
  grader,
  slug,
  onBack,
}: {
  grader: GraderDetail;
  slug: string;
  onBack: () => void;
}) {
  const only = grader.questions.length === 1 ? grader.questions[0] : undefined;
  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <BackButton onBack={onBack} />
        <Stack direction="horizontal" align="center" gap="snug" wrap>
          <Badge variant={PAGE_BADGE[grader.status]} size="sm">
            {GRADER_STATUS_LABEL[grader.status]}
          </Badge>
          <Heading variant="label">{graderName(grader)}</Heading>
          <Text size="caption" color="subtle" mono>
            {grader.name}
          </Text>
        </Stack>
        {grader.status === 'not_enough_runs' ? (
          <Text size="supporting" tabular>
            {grader.graded} graded runs
            <TooLarge n={grader.too_large} />
          </Text>
        ) : (
          <Text size="supporting" tabular>
            {grader.passed} of the last {grader.graded} runs passed
            {grader.min_pass_rate !== null && (
              <Text
                as="span"
                size="supporting"
                color={grader.status === 'failing' ? 'status-error' : 'subtle'}
              >
                {' '}
                · needs {grader.min_pass_rate}%
              </Text>
            )}
            <TooLarge n={grader.too_large} />
          </Text>
        )}
      </Stack>

      <Questions grader={grader} slug={slug} />

      {only &&
        (only.runs.length ? (
          <QuestionRuns grader={grader.name} q={only} slug={slug} />
        ) : (
          <Text size="supporting" color="muted" tabular>
            No failed or uncertain runs in the last {grader.graded}
          </Text>
        ))}
    </Stack>
  );
}

/** " · 2 too large to grade": the window's runs whose state did not fit the model. */
function TooLarge({ n }: { n: number }) {
  if (!n) return null;
  return (
    <Text as="span" size="supporting" color="subtle">
      {' '}
      · {n} too large to grade
    </Text>
  );
}

function BackButton({ onBack }: { onBack: () => void }) {
  return (
    <Stack direction="horizontal">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <Icon name="arrow-left" size="xs" />
        All Graders
      </Button>
    </Stack>
  );
}

function ReadError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <EmptyState
      icon="triangle-alert"
      title="Failed to load Graders"
      description={message}
      action={
        <Button variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  );
}

/** The Grader page, read from the API. */
function GraderRead({
  agentTypeId,
  name,
  slug,
  onBack,
}: {
  agentTypeId: string;
  name: string;
  slug: string;
  onBack: () => void;
}) {
  const { data, error, loading, reload } = useAgentGrader(agentTypeId, name);
  if (data) return <GraderPage grader={data} slug={slug} onBack={onBack} />;
  return (
    <Stack gap="comfortable">
      <BackButton onBack={onBack} />
      {loading ? (
        <Text size="supporting" color="muted">
          Loading Grader…
        </Text>
      ) : (
        <ReadError message={error ?? 'Grader not found'} onRetry={reload} />
      )}
    </Stack>
  );
}

/** The Grades tab: every Grader, or one Grader once picked. */
export function AgentGradesTab({
  agentTypeId,
  slug,
  grader,
  onGrader,
}: {
  agentTypeId: string;
  slug: string;
  /** The open Grader's name; undefined for the list. */
  grader: string | undefined;
  onGrader: (name: string | undefined) => void;
}) {
  // The list is read only while it shows.
  const { data, error, loading, reload } = useAgentGraders(grader ? undefined : agentTypeId);
  if (grader)
    return (
      <GraderRead
        key={grader}
        agentTypeId={agentTypeId}
        name={grader}
        slug={slug}
        onBack={() => onGrader(undefined)}
      />
    );
  if (loading)
    return (
      <Text size="supporting" color="muted">
        Loading Graders…
      </Text>
    );
  if (error) return <ReadError message={error} onRetry={reload} />;
  return <GradersList graders={data ?? []} onOpen={onGrader} />;
}
