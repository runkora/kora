'use client';

/**
 * @tier 3 — run-page-scoped. The run rail's Grades section: each Grader's verdict and one line per
 * question with its verdict and p, the question text on hover.
 */

import { Badge, Icon, Stack, Text, Tooltip, TooltipContent, TooltipTrigger } from '@runkora/hades';
import {
  GRADE_STATUS_LABEL,
  NOT_GRADED_YET,
  VERDICT_BADGE,
  VERDICT_ICON,
  VERDICT_LABEL,
} from '@runkora/app/(dashboard)/agents/_lib/grades';
import type { GradeAnswer, RunGrade } from '@runkora/shared/api';
import type { RunGrades } from '../_hooks/use-run-grades';

/** The stored option probabilities, shown on an uncertain choice or scale answer. */
function distribution(a: GradeAnswer) {
  return Object.entries(a.probabilities ?? {})
    .sort((x, y) => y[1] - x[1])
    .map(([k, v]) => `${k} ${v.toFixed(2)}`)
    .join(' · ');
}

function VerdictBadge({ grade }: { grade: RunGrade }) {
  if (grade.status !== 'graded' || !grade.verdict) {
    return (
      <Badge variant="muted" size="sm">
        {grade.status === 'graded' ? 'Graded' : GRADE_STATUS_LABEL[grade.status]}
      </Badge>
    );
  }
  return (
    <Badge variant={VERDICT_BADGE[grade.verdict]} size="sm">
      {VERDICT_LABEL[grade.verdict]}
    </Badge>
  );
}

const capText = (cap: number) =>
  cap === 0
    ? 'Tool result bodies left out'
    : `Tool results capped at ${cap.toLocaleString()} characters`;

/** Model, input tokens and cost summed over the Graders that called the model, and any state cap. */
function Footnote({ grades }: { grades: RunGrade[] }) {
  // A too-large state was never sent; any other grade with tokens made the call.
  const called = grades.filter((g) => g.status !== 'too_large' && g.input_tokens !== null);
  const caps = [
    ...new Set(grades.flatMap((g) => (g.state_cap === null ? [] : [g.state_cap]))),
  ].sort((a, b) => b - a);
  if (!called.length && !caps.length) return null;
  const models = [...new Set(called.map((g) => g.model_served ?? g.model))].join(', ');
  const tokens = called.reduce((n, g) => n + (g.input_tokens ?? 0), 0);
  // An unpriced model has no cost, so a sum with one missing would understate it.
  const cost = called.every((g) => g.cost_usd !== null)
    ? called.reduce((n, g) => n + (g.cost_usd ?? 0), 0)
    : null;
  return (
    <Stack gap="tight">
      {called.length > 0 && (
        <Text size="caption" color="muted" tabular>
          {models} · {tokens < 1000 ? tokens : `${Math.round(tokens / 1000)}k`} input tokens
          {cost !== null && ` · $${cost.toFixed(3)}`}
        </Text>
      )}
      {caps.map((cap) => (
        <Text key={cap} size="caption" color="muted" tabular>
          {capText(cap)}
        </Text>
      ))}
    </Stack>
  );
}

/** One rail line per question: verdict icon, name, p. The question text is on hover or focus. */
function RailQuestion({ a }: { a: GradeAnswer }) {
  const icon = VERDICT_ICON[a.verdict];
  return (
    <Stack gap="tight">
      <Tooltip>
        <TooltipTrigger asChild>
          <Stack direction="horizontal" align="center" justify="between" gap="snug" tabIndex={0}>
            <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
              <Icon name={icon.name} size="xs" color={icon.color} label={a.verdict} />
              <Text size="supporting" mono truncate>
                {a.question}
              </Text>
            </Stack>
            <Text size="caption" color="muted" tabular>
              {a.p === null ? VERDICT_LABEL.skipped : a.p.toFixed(2)}
            </Text>
          </Stack>
        </TooltipTrigger>
        <TooltipContent>{a.text}</TooltipContent>
      </Tooltip>
      {a.verdict === 'uncertain' && a.probabilities && (
        <Text size="caption" color="muted" tabular>
          {distribution(a)}
        </Text>
      )}
    </Stack>
  );
}

function GraderRow({ name, badge }: { name: string; badge: React.ReactNode }) {
  return (
    <Stack direction="horizontal" align="center" justify="between">
      <Text size="supporting" weight="emphasis">
        {name}
      </Text>
      {badge}
    </Stack>
  );
}

/** The Grades section of the run rail; nothing when the agent has no Graders. */
export function RailGrades({ grades }: { grades: RunGrades | null }) {
  const count = !grades ? 0 : grades.ended ? grades.grades.length : grades.graders.length;
  if (!grades || !count) return null;
  return (
    <Stack gap="snug">
      <Text size="caption" color="subtle">
        Grades
      </Text>
      <Stack gap="cozy">
        {grades.ended
          ? grades.grades.map((g) => (
              <Stack key={g.grader} gap="snug">
                <GraderRow name={g.grader} badge={<VerdictBadge grade={g} />} />
                {g.status === 'graded'
                  ? g.answers.map((a) => <RailQuestion key={a.question} a={a} />)
                  : // The badge names the status; only the stored applies p adds to it.
                    g.applies_p !== null && (
                      <Text size="caption" color="muted" tabular>
                        Applies p {g.applies_p.toFixed(2)}
                      </Text>
                    )}
              </Stack>
            ))
          : grades.graders.map((name) => (
              <GraderRow
                key={name}
                name={name}
                badge={
                  <Badge variant="muted" size="sm">
                    {NOT_GRADED_YET}
                  </Badge>
                }
              />
            ))}
      </Stack>
      {grades.ended && <Footnote grades={grades.grades} />}
    </Stack>
  );
}
