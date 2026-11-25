'use client';

/**
 * @tier 3 · agents/[slug]-scoped. One Grader on the Grades tab: its status, what it asks, and each
 * question's failed or uncertain runs. Every string is a stored field or a count of them.
 */

import { useState } from 'react';
import NextLink from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Anchor,
  Badge,
  Button,
  DataTable,
  Heading,
  Icon,
  Stack,
  Text,
  type DataTableColumn,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing/routes';
import type {
  GraderAnswerCount,
  GraderDetail,
  GraderFlaggedRun,
  GraderQuestion,
  GraderStatus,
} from '@runkora/shared/api';
import {
  GRADER_STATUS_LABEL,
  VERDICT_BADGE,
  VERDICT_LABEL,
  gradeRunsHref,
  graderName,
  passesOn,
  QUESTION_TYPE_LABEL,
  WINDOW,
  answerLabel,
} from '../../_lib/grades';
import { formatRelativeTime, shortRelativeTime } from '../../_types';

/** Runs listed before "Show all". */
const RUNS_SHOWN = 5;

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

/** "Threshold pass 0.7, fail 0.3". */
const band = (t: { pass: number; fail: number }) => `Threshold pass ${t.pass}, fail ${t.fail}`;

/** The Grader's own spec fields: appliesWhen when set, threshold, model and short version. */
const specLine = (g: GraderDetail) =>
  [
    g.applies_when && `Applies when: ${g.applies_when}`,
    band(g.threshold),
    `Model ${g.model}`,
    `Version ${g.version.slice(0, 7)}`,
  ]
    .filter(Boolean)
    .join(' · ');

/** "Scale · Passes on …", then its skipOn and its own threshold when set. */
function questionLine(q: GraderQuestion): string {
  const answers = q.options ?? q.levels ?? [];
  const skips = (q.skip_on ?? []).map((n) => answers.find((a) => a.name === n)?.description || n);
  return [
    QUESTION_TYPE_LABEL[q.type],
    passesOn(q),
    skips.length && `Skips on ${skips.join(', ')}`,
    q.threshold && band(q.threshold),
  ]
    .filter(Boolean)
    .join(' · ');
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

/** Every answer a question can get, whether it passes or skips, and how many window runs had it. */
function Answers({ q }: { q: GraderQuestion }) {
  const columns: DataTableColumn<GraderAnswerCount>[] = [
    {
      id: 'passes',
      header: '',
      gridWidth: '1.5rem',
      cell: (a) =>
        a.passes ? (
          <Icon name="circle-check" size="sm" color="status-healthy" label="Passes" />
        ) : null,
    },
    {
      id: 'answer',
      header: 'Answer',
      gridWidth: 'minmax(0, 1fr)',
      cell: (a) => (
        <Text size="supporting">
          {answerLabel(q, a)}
          {a.skips && (
            <Text as="span" size="supporting" color="subtle">
              {' '}
              · skips the question
            </Text>
          )}
        </Text>
      ),
    },
    {
      id: 'count',
      header: `Runs (last ${WINDOW})`,
      gridWidth: '8rem',
      align: 'right',
      cell: (a) => (
        <Text size="supporting" tabular>
          {a.count}
        </Text>
      ),
    },
  ];
  return (
    <DataTable
      data={q.answers}
      columns={columns}
      getRowKey={(a) => a.name}
      columnToggle={false}
      noun="answers"
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          Answers
        </Text>
      }
    />
  );
}

/** One row per question with its type, pass condition and verdict counts; a row opens to its answers and runs. */
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
            {questionLine(q)}
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
          : (q) => (
              <Stack gap="snug">
                <Answers q={q} />
                {q.runs.length ? (
                  <QuestionRuns grader={grader.name} q={q} slug={slug} />
                ) : (
                  <Text size="supporting" color="subtle">
                    No failed or uncertain runs
                  </Text>
                )}
              </Stack>
            )
      }
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          What it asks
        </Text>
      }
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
        <Text size="caption" color="subtle" tabular>
          {specLine(grader)}
        </Text>
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

      {only && <Answers q={only} />}
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

export function BackButton({ onBack }: { onBack: () => void }) {
  return (
    <Stack direction="horizontal">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <Icon name="arrow-left" size="xs" />
        All Graders
      </Button>
    </Stack>
  );
}
