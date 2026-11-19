'use client';

// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/**
 * @tier 3 · agents/[slug]-scoped. Proposed: the Grades tab. `GradersList` shows each Grader's pass rate
 * over its last 30 graded runs against its `minPassRate`, failing first; `GraderPage` shows one
 * Grader's questions and its failed or uncertain runs. `GradesTab` switches between them. Every
 * string is a stored field or a count over stored grades.
 */

import { useMemo, useState } from 'react';
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  type DataTableColumn,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing/routes';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '../../../triggers/_types/pagination';
import type { Grade, GraderSeries, GraderSpec, QuestionSpec } from '../../_lib/grades-proposed';

const time = (iso: string) =>
  new Date(iso).toLocaleString('en-GB', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

/** A Grader is judged on its last `WINDOW` graded runs, whatever the agent config. */
const WINDOW = 30;
/** Below this many graded runs in the window, a Grader is not judged. */
const MIN_RUNS = 15;
/** A question uncertain on at least this share of the window shows its uncertain count. */
const UNCERTAIN_SHOWN = 0.15;
/** Runs listed before "Show all". */
const RUNS_SHOWN = 5;

// ── State ────────────────────────────────────────────────────────────────────

/** `none`: no `minPassRate`, so no pass or fail. `few`: under `MIN_RUNS` graded runs. */
type Status = 'failing' | 'passing' | 'none' | 'few';

interface GraderState {
  spec: GraderSpec;
  /** The last `WINDOW` graded runs, newest first. */
  recent: Grade[];
  pass: number;
  /** Pass rate over `recent`, in percent. */
  rate: number;
  status: Status;
}

function stateOf({ spec, grades }: GraderSeries): GraderState {
  const recent = grades
    .filter((g) => g.status === 'graded')
    .sort((a, b) => b.runStartedAt.localeCompare(a.runStartedAt))
    .slice(0, WINDOW);
  const pass = recent.filter((g) => g.verdict === 'pass').length;
  const rate = recent.length ? Math.round((pass / recent.length) * 100) : 0;
  const status: Status =
    recent.length < MIN_RUNS
      ? 'few'
      : spec.minPassRate === undefined
        ? 'none'
        : rate < spec.minPassRate
          ? 'failing'
          : 'passing';
  return { spec, recent, pass, rate, status };
}

const ORDER: Record<Status, number> = { failing: 0, none: 1, passing: 2, few: 3 };

/** Failing first, furthest below `minPassRate` first; then the rest by pass rate; `few` last. */
function rank(states: GraderState[]) {
  const gap = (s: GraderState) => (s.spec.minPassRate ?? 0) - s.rate;
  return [...states].sort(
    (a, b) =>
      ORDER[a.status] - ORDER[b.status] ||
      (a.status === 'failing' ? gap(b) - gap(a) : 0) ||
      a.rate - b.rate,
  );
}

/** "52% passed · needs 90%"; the `minPassRate` part only when the spec sets one. */
function RateText({ s }: { s: GraderState }) {
  const min = s.spec.minPassRate;
  return (
    <Text size="supporting" tabular color={s.status === 'failing' ? 'status-error' : 'foreground'}>
      {s.rate}% passed
      {min !== undefined && (
        <Text as="span" size="supporting" color="subtle">
          {' '}
          · needs {min}%
        </Text>
      )}
    </Text>
  );
}

/** "14 graded runs · needs 15 to judge" */
const fewText = (s: GraderState) => `${s.recent.length} graded runs · needs ${MIN_RUNS} to judge`;

// ── Runs ─────────────────────────────────────────────────────────────────────

/** A run whose answer to one question was fail or uncertain. */
interface FlaggedRun {
  g: Grade;
  verdict: 'fail' | 'uncertain';
}

/**
 * The agent's Runs tab filtered to one question's failed runs. Needs backend support: the runs list
 * does not filter by grade yet.
 */
function AllFailedLink({
  slug,
  grader,
  question,
}: {
  slug: string;
  grader: string;
  question: string;
}) {
  const query = new URLSearchParams({ tab: 'runs', grader, question, verdict: 'fail' });
  return (
    <Stack direction="horizontal">
      <Anchor asChild size="sm">
        <NextLink href={`${APP_ROUTES.agents.detail(slug)}?${query}`}>
          All failed runs for this question →
        </NextLink>
      </Anchor>
    </Stack>
  );
}

/** Runs newest first, each with its verdict; a row opens the run page. */
function RunList({ runs, slug }: { runs: FlaggedRun[]; slug: string }) {
  const router = useRouter();
  const [all, setAll] = useState(false);
  const sorted = [...runs].sort((a, b) => b.g.runStartedAt.localeCompare(a.g.runStartedAt));
  const shown = all ? sorted : sorted.slice(0, RUNS_SHOWN);
  const columns: DataTableColumn<FlaggedRun>[] = [
    {
      id: 'verdict',
      header: 'Verdict',
      gridWidth: '6.5rem',
      cell: (r) => (
        <Badge variant={r.verdict === 'fail' ? 'error' : 'degraded'} size="sm">
          {r.verdict === 'fail' ? 'Fail' : 'Uncertain'}
        </Badge>
      ),
    },
    {
      id: 'time',
      header: 'Started',
      gridWidth: '8rem',
      cell: (r) => (
        <Text size="supporting" color="subtle" tabular>
          {time(r.g.runStartedAt)}
        </Text>
      ),
    },
    {
      id: 'title',
      header: 'Run',
      gridWidth: 'minmax(0, 1fr)',
      cell: (r) => (
        <Text size="supporting" truncate>
          {r.g.runTitle ?? r.g.runId}
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
      getRowKey={(r) => r.g.runId}
      hideHeader
      columnToggle={false}
      noun="runs"
      toolbarTitle={
        <Text size="supporting" weight="emphasis" tabular>
          Failed or uncertain runs ({sorted.length})
        </Text>
      }
      toolbarActions={
        sorted.length > RUNS_SHOWN ? (
          <Button variant="ghost" size="sm" onClick={() => setAll((a) => !a)}>
            {all ? 'Show fewer' : `Show all ${sorted.length}`}
          </Button>
        ) : undefined
      }
      onRowClick={(r) => router.push(APP_ROUTES.agents.run(slug, r.g.runId))}
      rowLabel={(r) => `Open run ${r.g.runTitle ?? r.g.runId}`}
    />
  );
}

// ── Level 1: every Grader ────────────────────────────────────────────────────

const STATUS_ICON = {
  failing: { name: 'circle-x', color: 'status-error', label: 'Failing' },
  passing: { name: 'circle-check', color: 'status-healthy', label: 'Passing' },
  none: { name: 'minus', color: 'subtle', label: 'No minPassRate' },
  few: { name: 'clock', color: 'subtle', label: 'Not judged' },
} as const;

type Filter = 'all' | 'failing' | 'passing' | 'none';

const FILTER_LABEL: Record<Filter, string> = {
  all: 'All',
  failing: 'Failing',
  passing: 'Passing',
  none: 'No minimum',
};

const matches = (s: GraderState, q: string) =>
  s.spec.name.toLowerCase().includes(q) || (s.spec.description ?? '').toLowerCase().includes(q);

const COLUMNS: DataTableColumn<GraderState>[] = [
  {
    id: 'grader',
    header: 'Grader',
    gridWidth: 'minmax(0, 1fr)',
    cell: (s) => {
      const icon = STATUS_ICON[s.status];
      return (
        <Stack direction="horizontal" align="start" gap="snug" minWidth="none">
          <Icon name={icon.name} size="sm" color={icon.color} label={icon.label} />
          <Stack gap="tight" minWidth="none">
            <Text
              size="supporting"
              weight={s.status === 'failing' ? 'emphasis' : 'regular'}
              truncate
            >
              {s.spec.description ?? s.spec.name}
            </Text>
            <Text size="caption" color="subtle" mono>
              {s.spec.name}
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
    cell: (s) =>
      s.status === 'few' ? (
        <Text size="supporting" color="subtle" tabular>
          Needs {MIN_RUNS} to judge
        </Text>
      ) : (
        <RateText s={s} />
      ),
  },
  {
    id: 'runs',
    header: 'Graded runs',
    gridWidth: '7rem',
    align: 'right',
    cell: (s) => (
      <Text size="supporting" color="muted" tabular>
        {s.recent.length}
      </Text>
    ),
  },
];

export function GradersList({
  series,
  onOpen,
}: {
  series: GraderSeries[];
  onOpen: (name: string) => void;
}) {
  const [filter, setFilter] = useState<Filter>('all');
  const states = useMemo(() => rank(series.map(stateOf)), [series]);
  if (!states.length)
    return (
      <EmptyState
        icon="file-check"
        title="No Graders"
        description="This agent has no Grader resources"
      />
    );

  const count = (f: Filter) =>
    f === 'all' ? states.length : states.filter((s) => s.status === f).length;
  const rows = filter === 'all' ? states : states.filter((s) => s.status === filter);

  return (
    <DataTable
      // A new filter starts again from page one.
      key={filter}
      data={rows}
      columns={COLUMNS}
      getRowKey={(s) => s.spec.name}
      columnToggle={false}
      noun="Graders"
      onRowClick={(s) => onOpen(s.spec.name)}
      rowLabel={(s) => `Open ${s.spec.description ?? s.spec.name}`}
      searchFn={(s, q) => matches(s, q.toLowerCase())}
      searchPlaceholder="Search Graders…"
      emptyMessage={`No ${FILTER_LABEL[filter].toLowerCase()} Graders`}
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
            {count('failing')} of {states.length} Graders failing
          </Text>
          <Text size="caption" color="subtle" tabular>
            Last {WINDOW} graded runs per Grader
          </Text>
        </Stack>
      }
    />
  );
}

// ── Level 2: one Grader ──────────────────────────────────────────────────────

/** "Passes on Yes", "Passes on No", or "Passes on" and the accepted options' descriptions. */
function passesOn(q: QuestionSpec) {
  if (q.type === 'boolean') return `Passes on ${q.expect === false ? 'No' : 'Yes'}`;
  const names = (q.accept ?? []).map((n) => q.options?.find((o) => o.name === n)?.description ?? n);
  return `Passes on ${names.join(', ')}`;
}

/** The spec's fields: `appliesWhen`, each question with its `expect` or `accept`, version and model. */
function SpecLink({ spec }: { spec: GraderSpec }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Anchor href="#" size="sm" onClick={(e) => e.preventDefault()}>
          View spec
        </Anchor>
      </PopoverTrigger>
      <PopoverContent wide>
        <Stack gap="cozy">
          {spec.appliesWhen && (
            <Stack gap="tight">
              <Text size="caption" color="subtle">
                Applies when
              </Text>
              <Text size="supporting">{spec.appliesWhen}</Text>
            </Stack>
          )}
          <List gap="cozy">
            {spec.questions.map((q) => (
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
            Version {spec.version} · {spec.model}
          </Text>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

const PAGE_STATUS = {
  failing: { badge: 'error', text: 'Failing' },
  passing: { badge: 'healthy', text: 'Passing' },
  none: { badge: 'muted', text: 'No minPassRate' },
  few: { badge: 'muted', text: 'Not judged' },
} as const;

interface QuestionRow {
  q: QuestionSpec;
  flagged: FlaggedRun[];
  fail: number;
  uncertain: number;
}

/** "Fail 5 · Uncertain 2", leaving out a zero. */
const counts = (r: QuestionRow) =>
  [r.fail && `Fail ${r.fail}`, r.uncertain && `Uncertain ${r.uncertain}`]
    .filter(Boolean)
    .join(' · ');

/** One row per question with its pass condition and verdict counts; a row with runs opens to them. */
function Questions({
  rows,
  spec,
  slug,
  total,
  single,
}: {
  rows: QuestionRow[];
  spec: GraderSpec;
  slug: string;
  /** Graded runs in the window, for the uncertain share. */
  total: number;
  /** One question: no counts, its runs follow as their own list. */
  single: boolean;
}) {
  const first = rows.find((r) => r.flagged.length)?.q.name;
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set(first && !single ? [first] : []));
  const columns: DataTableColumn<QuestionRow>[] = [
    {
      id: 'question',
      header: 'Question',
      gridWidth: 'minmax(0, 1fr)',
      wrap: true,
      cell: (r) => (
        <Stack gap="tight" minWidth="none">
          <Tooltip>
            <TooltipTrigger asChild>
              <Text size="supporting">{r.q.question}</Text>
            </TooltipTrigger>
            <TooltipContent>{r.q.name}</TooltipContent>
          </Tooltip>
          <Text size="caption" color="subtle">
            {passesOn(r.q)}
          </Text>
          {total > 0 && r.uncertain / total >= UNCERTAIN_SHOWN && (
            <Text size="caption" color="status-degraded" tabular>
              Uncertain on {r.uncertain} of {total} runs
            </Text>
          )}
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
      cell: (r) =>
        r.flagged.length ? (
          <Stack direction="horizontal" align="center" justify="end" gap="snug">
            <Text size="supporting" tabular>
              {counts(r)}
            </Text>
            <Icon
              name={open.has(r.q.name) ? 'chevron-down' : 'chevron-right'}
              size="xs"
              color="subtle"
            />
          </Stack>
        ) : (
          <Text size="supporting" color="subtle">
            All pass
          </Text>
        ),
    });
  return (
    <DataTable
      data={rows}
      columns={columns}
      getRowKey={(r) => r.q.name}
      hideHeader
      columnToggle={false}
      noun="questions"
      expandedKeys={open}
      onExpandedChange={(key, expanded) => {
        // A question with no failed or uncertain runs has nothing to open.
        if (single || !rows.find((r) => r.q.name === key)?.flagged.length) return;
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
          : (r) =>
              r.flagged.length ? (
                <Stack gap="snug">
                  <RunList runs={r.flagged} slug={slug} />
                  {r.fail > 0 && (
                    <AllFailedLink slug={slug} grader={spec.name} question={r.q.name} />
                  )}
                </Stack>
              ) : null
      }
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          Questions
        </Text>
      }
      toolbarActions={<SpecLink spec={spec} />}
    />
  );
}

export function GraderPage({
  series,
  slug,
  onBack,
}: {
  series: GraderSeries;
  slug: string;
  onBack: () => void;
}) {
  const s = useMemo(() => stateOf(series), [series]);
  const badge = PAGE_STATUS[s.status];
  const rows: QuestionRow[] = s.spec.questions
    .map((q) => {
      const flagged = s.recent.flatMap((g): FlaggedRun[] => {
        const v = g.answers.find((a) => a.question === q.name)?.verdict;
        return v === 'fail' || v === 'uncertain' ? [{ g, verdict: v }] : [];
      });
      const uncertain = flagged.filter((r) => r.verdict === 'uncertain').length;
      return { q, flagged, fail: flagged.length - uncertain, uncertain };
    })
    .sort((a, b) => b.flagged.length - a.flagged.length);
  const single = rows.length === 1;
  const only = rows[0];

  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <Stack direction="horizontal">
          <Button variant="ghost" size="sm" onClick={onBack}>
            <Icon name="arrow-left" size="xs" />
            All Graders
          </Button>
        </Stack>
        <Stack direction="horizontal" align="center" gap="snug" wrap>
          <Badge variant={badge.badge} size="sm">
            {badge.text}
          </Badge>
          <Heading variant="label">{s.spec.description ?? s.spec.name}</Heading>
          <Text size="caption" color="subtle" mono>
            {s.spec.name}
          </Text>
        </Stack>
        {s.status === 'few' ? (
          <Text size="supporting" tabular>
            {fewText(s)}
          </Text>
        ) : (
          <Text size="supporting" tabular>
            {s.pass} of the last {s.recent.length} runs passed
            {s.spec.minPassRate !== undefined && (
              <Text
                as="span"
                size="supporting"
                color={s.status === 'failing' ? 'status-error' : 'subtle'}
              >
                {' '}
                · needs {s.spec.minPassRate}%
              </Text>
            )}
          </Text>
        )}
      </Stack>

      <Questions rows={rows} spec={s.spec} slug={slug} total={s.recent.length} single={single} />

      {single &&
        only &&
        (only.flagged.length ? (
          <Stack gap="snug">
            <RunList runs={only.flagged} slug={slug} />
            {only.fail > 0 && (
              <AllFailedLink slug={slug} grader={s.spec.name} question={only.q.name} />
            )}
          </Stack>
        ) : (
          <Text size="supporting" color="muted" tabular>
            No failed or uncertain runs in the last {s.recent.length}
          </Text>
        ))}
    </Stack>
  );
}

/** The Grades tab: every Grader, or one Grader once picked. */
export function GradesTab({
  series,
  slug,
  initialGrader,
}: {
  series: GraderSeries[];
  slug: string;
  initialGrader?: string;
}) {
  const [open, setOpen] = useState(initialGrader);
  const picked = series.find((s) => s.spec.name === open);
  if (picked)
    return (
      <GraderPage
        key={picked.spec.name}
        series={picked}
        slug={slug}
        onBack={() => setOpen(undefined)}
      />
    );
  return <GradersList series={series} onOpen={setOpen} />;
}
