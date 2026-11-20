'use client';

// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/**
 * @tier 3 — run-page-scoped. Proposed: a finished run's grades as a rail section, one line per
 * question.
 */

import { Badge, Icon, Stack, Text, Tooltip, TooltipContent, TooltipTrigger } from '@runkora/hades';
import {
  VERDICT_BADGE,
  VERDICT_ICON,
  VERDICT_LABEL,
  specFor,
  type Answer,
  type Grade,
  type QuestionSpec,
} from '@runkora/app/(dashboard)/agents/_lib/grades-proposed';

const STATUS_TEXT: Partial<Record<Grade['status'], string>> = {
  not_applicable: 'Not applicable',
  applies_uncertain: 'Applies: uncertain',
  too_large: 'Too large to grade',
  error: 'Grading failed',
  pending: 'Not graded yet',
};

const ICON_COLOR = {
  pass: 'status-healthy',
  fail: 'status-error',
  uncertain: 'status-degraded',
} as const;

/** The stored option probabilities, shown on an uncertain choice answer. */
function distribution(a: Answer) {
  return Object.entries(a.probabilities ?? {})
    .sort((x, y) => y[1] - x[1])
    .map(([k, v]) => `${k} ${v.toFixed(2)}`)
    .join(' · ');
}

function VerdictBadge({ grade }: { grade: Grade }) {
  if (grade.status !== 'graded' || !grade.verdict) {
    return (
      <Badge variant="muted" size="sm">
        {STATUS_TEXT[grade.status] ?? grade.status}
      </Badge>
    );
  }
  return (
    <Badge variant={VERDICT_BADGE[grade.verdict]} size="sm">
      {VERDICT_LABEL[grade.verdict]}
    </Badge>
  );
}

function Footnote({ grades }: { grades: Grade[] }) {
  const graded = grades.filter((g) => g.status === 'graded');
  if (!graded.length) return null;
  const cost = graded.reduce((n, g) => n + g.cost, 0);
  const cap = graded.find((g) => g.stateCap)?.stateCap;
  return (
    <Stack gap="tight">
      <Text size="caption" color="muted" tabular>
        {specFor(graded[0]?.grader ?? '').model} ·{' '}
        {Math.round((graded[0]?.inputTokens ?? 0) / 1000)}k input tokens · ${cost.toFixed(3)}
      </Text>
      {cap && (
        <Text size="caption" color="muted" tabular>
          Tool results capped at {cap.toLocaleString()} characters
        </Text>
      )}
    </Stack>
  );
}

/** One rail line per question: verdict icon, name, p. The question text is on hover. */
function RailQuestion({ q, a }: { q: QuestionSpec | undefined; a: Answer }) {
  return (
    <Stack gap="tight">
      <Tooltip>
        <TooltipTrigger asChild>
          <Stack direction="horizontal" align="center" justify="between" gap="snug">
            <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
              <Icon
                name={VERDICT_ICON[a.verdict]}
                size="xs"
                color={ICON_COLOR[a.verdict]}
                label={a.verdict}
              />
              <Text size="supporting" mono truncate>
                {a.question}
              </Text>
            </Stack>
            <Text size="caption" color="muted" tabular>
              {a.p.toFixed(2)}
            </Text>
          </Stack>
        </TooltipTrigger>
        <TooltipContent>{q?.question ?? a.question}</TooltipContent>
      </Tooltip>
      {a.verdict === 'uncertain' && a.probabilities && (
        <Text size="caption" color="muted" tabular>
          {distribution(a)}
        </Text>
      )}
    </Stack>
  );
}

/** The Grades section of the run rail. */
export function RailGrades({ grades }: { grades: Grade[] }) {
  if (!grades.length) return null;
  return (
    <Stack gap="snug">
      <Stack direction="horizontal" justify="between" align="center">
        <Text size="caption" color="subtle">
          Grades
        </Text>
      </Stack>
      <Stack gap="cozy">
        {grades.map((g) => {
          const spec = specFor(g.grader);
          return (
            <Stack key={g.grader} gap="snug">
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="supporting" weight="emphasis">
                  {g.grader}
                </Text>
                <VerdictBadge grade={g} />
              </Stack>
              {g.status === 'graded'
                ? g.answers.map((a) => (
                    <RailQuestion
                      key={a.question}
                      q={spec.questions.find((q) => q.name === a.question)}
                      a={a}
                    />
                  ))
                : // The badge already names the status; only the stored applies p adds to it.
                  g.status === 'not_applicable' &&
                  g.appliesP !== undefined && (
                    <Text size="caption" color="muted" tabular>
                      Applies p {g.appliesP.toFixed(2)}
                    </Text>
                  )}
            </Stack>
          );
        })}
      </Stack>
      <Footnote grades={grades} />
    </Stack>
  );
}
