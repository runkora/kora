/**
 * Story data for the Grades tab: twelve Graders of one agent as the list endpoint answers them,
 * failing first, and the detail of two of them. `summary-accurate` has five questions, one a choice
 * and one a scale. `ci-read` has one question. Each question's answer counts cover the 30 graded runs.
 */

import type {
  GraderAnswerCount,
  GraderDetail,
  GraderFlaggedRun,
  GraderStatus,
  GraderSummary,
  GradeVerdict,
} from '@runkora/shared/api';

const summary = (
  name: string,
  description: string,
  status: GraderStatus,
  [passed, graded]: [number, number],
  min_pass_rate: number | null,
  too_large: number,
  version: string,
): GraderSummary => ({
  name,
  description,
  min_pass_rate,
  version,
  status,
  passed,
  graded,
  too_large,
});

const SUMMARIES: GraderSummary[] = [
  summary(
    'review-posted',
    'Posts exactly one review on the pull request it was asked about.',
    'failing',
    [10, 30],
    90,
    0,
    '127202a',
  ),
  summary(
    'reads-callers',
    'Reads the code that calls a changed function before commenting on it.',
    'failing',
    [11, 30],
    90,
    1,
    '14cd5a1',
  ),
  summary(
    'summary-accurate',
    'Summarises the change correctly in the first paragraph.',
    'failing',
    [17, 30],
    80,
    5,
    '5edcd13',
  ),
  summary(
    'recommendation-clear',
    'Ends with a clear merge or change-request recommendation.',
    'failing',
    [25, 30],
    85,
    0,
    '34159b3',
  ),
  summary(
    'no-push',
    'Never pushes commits to the branch under review.',
    'failing',
    [27, 30],
    95,
    1,
    '7b0633a',
  ),
  summary(
    'scope-kept',
    'Reviews only the files in the pull request.',
    'failing',
    [28, 30],
    95,
    1,
    'e291b49',
  ),
  summary(
    'actionable-comments',
    'Leaves comments that point to a line and say what to change.',
    'passing',
    [24, 30],
    70,
    1,
    '7670fa9',
  ),
  summary(
    'no-approval',
    'Never approves or merges a pull request.',
    'passing',
    [27, 30],
    85,
    2,
    '3cc9c4c',
  ),
  summary(
    'tests-checked',
    'Checks whether the change is covered by tests and says so.',
    'passing',
    [28, 30],
    85,
    2,
    '45f5b00',
  ),
  summary(
    'tone',
    'Writes comments that are direct and free of sarcasm.',
    'no_minimum',
    [28, 30],
    null,
    3,
    '366972a',
  ),
  summary(
    'ci-read',
    'Reads the CI result before reviewing.',
    'passing',
    [29, 30],
    90,
    1,
    '2ae53e9',
  ),
  summary(
    'security-flags',
    'Flags secrets, injection and unsafe permissions in the diff.',
    'not_enough_runs',
    [5, 5],
    90,
    0,
    '6b39f64',
  ),
];

/** Extra Graders for paging, each passing on every run of its window. */
const MORE: [name: string, description: string][] = [
  ['names-files', 'Names the files it changed in its summary.'],
  ['links-issue', 'Links the issue the pull request fixes.'],
  ['checks-migrations', 'Checks database migrations for a down step.'],
  ['flags-todo', 'Flags new TODO comments in the diff.'],
  ['checks-errors', 'Checks that new errors are wrapped with context.'],
  ['checks-logging', 'Checks that new logs carry a request id.'],
  ['checks-docs', 'Asks for docs when a public API changes.'],
  ['checks-config', 'Checks new config keys have defaults.'],
  ['no-secrets-echo', 'Never repeats a secret from the diff in a comment.'],
  ['checks-deps', 'Checks new dependencies are pinned.'],
  ['checks-licence', 'Checks new dependencies have a compatible licence.'],
  ['checks-tests-run', 'Reads the test output before approving behaviour.'],
  ['short-comments', 'Keeps each comment under five sentences.'],
  ['no-style-only', 'Raises more than style issues.'],
  ['checks-api-break', 'Flags breaking API changes.'],
  ['checks-perf', 'Flags loops that call the network.'],
  ['checks-null', 'Flags unchecked nil or null values.'],
  ['checks-sql', 'Flags SQL built from strings.'],
  ['checks-retries', 'Checks retries have a limit.'],
  ['checks-timeouts', 'Checks outbound calls have a timeout.'],
  ['checks-feature-flag', 'Asks for a flag on risky changes.'],
  ['checks-rollback', 'Asks how a risky change is rolled back.'],
  ['checks-metrics', 'Asks for a metric on new code paths.'],
  ['checks-i18n', 'Flags user-facing strings not in the catalogue.'],
  ['checks-a11y', 'Flags new buttons without a label.'],
  ['checks-dead-code', 'Flags code the change leaves unused.'],
  ['replies-in-thread', 'Replies to author questions in their thread.'],
  ['no-duplicate-review', 'Does not post a second review on the same commit.'],
];

const run = (
  n: number,
  title: string,
  started_at: string,
  verdict: GradeVerdict,
): GraderFlaggedRun => ({
  run_id: `run_${String(n).padStart(4, '0')}`,
  title,
  started_at,
  verdict,
});

/** A choice option or scale level with its window count; `passes` or `skips` marks accept or skipOn. */
const answer = (
  name: string,
  description: string,
  count: number,
  role?: 'passes' | 'skips',
): GraderAnswerCount => ({
  name,
  description,
  passes: role === 'passes',
  skips: role === 'skips',
  count,
});

/** A boolean's answers for `expect: true`, with the window's yes and no counts. */
const yesNo = (yes: number, no: number): GraderAnswerCount[] => [
  answer('yes', '', yes, 'passes'),
  answer('no', '', no),
];

const SPEC = { model: 'jev-1-14', threshold: { pass: 0.7, fail: 0.3 } };

const DETAILS: GraderDetail[] = [
  {
    ...SUMMARIES[2]!,
    ...SPEC,
    questions: [
      {
        name: 'summary-correct',
        type: 'boolean',
        question: 'Does the summary match the diff?',
        expect: true,
        answers: yesNo(25, 5),
        failed: 2,
        uncertain: 7,
        runs: [
          run(79, 'Review docs/1479: index Slack threads', '2026-11-16T18:52:40Z', 'fail'),
          run(76, 'Review factory/1476: log Slack threads', '2026-11-15T16:32:11Z', 'uncertain'),
          run(73, 'Review gateway/1473: cache Slack threads', '2026-11-14T14:11:42Z', 'uncertain'),
          run(72, 'Review design-system/1472: retry Slack threads', '2026-11-14T05:24:52Z', 'fail'),
          run(70, 'Review platform/1470: index Grader specs', '2026-11-13T11:51:13Z', 'uncertain'),
          run(
            61,
            'Review factory/1461: index trigger workers',
            '2026-11-10T04:49:45Z',
            'uncertain',
          ),
          run(
            57,
            'Review design-system/1457: validate trigger workers',
            '2026-11-08T17:42:26Z',
            'uncertain',
          ),
          run(
            56,
            'Review factory/1456: paginate trigger workers',
            '2026-11-08T08:55:36Z',
            'uncertain',
          ),
          run(54, 'Review docs/1454: retry trigger workers', '2026-11-07T15:21:57Z', 'uncertain'),
        ],
      },
      {
        name: 'no-invented-files',
        type: 'boolean',
        question: 'Does the summary mention only files in the diff?',
        expect: true,
        answers: yesNo(29, 1),
        failed: 1,
        uncertain: 1,
        runs: [
          run(
            82,
            'Review design-system/1482: cache the agents list',
            '2026-11-17T21:13:10Z',
            'uncertain',
          ),
          run(58, 'Review gateway/1458: log trigger workers', '2026-11-09T02:29:16Z', 'fail'),
        ],
      },
      {
        name: 'has-summary',
        type: 'boolean',
        question: 'Does the review start with a summary of the change?',
        expect: true,
        answers: yesNo(30, 0),
        failed: 0,
        uncertain: 1,
        runs: [
          run(
            81,
            'Review factory/1481: retry the agents list',
            '2026-11-17T12:26:20Z',
            'uncertain',
          ),
        ],
      },
      {
        name: 'length',
        type: 'scale',
        question: 'How long is the summary?',
        levels: [
          { name: 'no-summary', description: 'There is no summary' },
          { name: 'short', description: 'One or two sentences' },
          { name: 'medium', description: 'Three or four sentences' },
          { name: 'long', description: 'Five or more sentences' },
        ],
        accept: ['short', 'medium'],
        skip_on: ['no-summary'],
        answers: [
          answer('no-summary', 'There is no summary', 0, 'skips'),
          answer('short', 'One or two sentences', 18, 'passes'),
          answer('medium', 'Three or four sentences', 11, 'passes'),
          answer('long', 'Five or more sentences', 1),
        ],
        failed: 1,
        uncertain: 0,
        runs: [
          run(55, 'Review platform/1455: cache trigger workers', '2026-11-08T00:08:46Z', 'fail'),
        ],
      },
      {
        name: 'riskiest-part',
        type: 'choice',
        question: 'Which part of the change does the summary name as the riskiest?',
        options: [
          { name: 'migration', description: 'A database migration' },
          { name: 'api', description: 'A public API change' },
          { name: 'config', description: 'A config change' },
          { name: 'none', description: 'No part is named' },
        ],
        accept: ['migration', 'api', 'config'],
        answers: [
          answer('migration', 'A database migration', 9, 'passes'),
          answer('api', 'A public API change', 14, 'passes'),
          answer('config', 'A config change', 7, 'passes'),
          answer('none', 'No part is named', 0),
        ],
        failed: 0,
        uncertain: 0,
        runs: [],
      },
    ],
  },
  {
    ...SUMMARIES[10]!,
    ...SPEC,
    questions: [
      {
        name: 'read-ci',
        type: 'boolean',
        question: 'Did the agent read the CI checks for the pull request?',
        expect: true,
        answers: yesNo(29, 1),
        failed: 1,
        uncertain: 0,
        runs: [
          run(72, 'Review design-system/1472: retry Slack threads', '2026-11-14T05:24:52Z', 'fail'),
        ],
      },
    ],
  },
];

/** A fleet's Graders as the list and detail endpoints answer them. */
export interface GraderFleet {
  list: GraderSummary[];
  detail: Record<string, GraderDetail>;
}

const DETAIL = Object.fromEntries(DETAILS.map((d) => [d.name, d]));

/** Every seventh extra Grader sets no minimum. */
const MANY = MORE.map(([name, description], i) => {
  const min = i % 7 === 2 ? null : 80;
  return summary(
    name,
    description,
    min === null ? 'no_minimum' : 'passing',
    [30, 30],
    min,
    0,
    `e${String(i).padStart(6, '0')}`,
  );
});

/** Twelve Graders; `many` adds 28 more for paging; `none` has no Graders. */
export function graderFleet(mode: 'twelve' | 'many' | 'none' = 'twelve'): GraderFleet {
  if (mode === 'none') return { list: [], detail: {} };
  return { list: mode === 'many' ? [...SUMMARIES, ...MANY] : SUMMARIES, detail: DETAIL };
}
