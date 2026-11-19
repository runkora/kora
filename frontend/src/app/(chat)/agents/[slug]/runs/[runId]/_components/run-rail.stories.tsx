import type { Meta, StoryObj } from '@storybook/react';
import { Grid, Heading, Stack, TooltipProvider } from '@runkora/hades';
import { doneRun, runStory, runningRun, slackRun } from '../_lib/run-page-mock-data';
import type { MockRun } from '../_lib/run-page-mock-data';
import { RUN_GRADES } from '../_lib/run-grades-mock-data';
import { RunProvider, useRunContext } from '../_context/run-context';
import { toRunView } from '../_lib/to-run-view';
import { RunRail } from './run-rail';

/** The run page's right rail: the Code Reviewer mid-run and finished, a long Slack session, and its grades. */
const meta: Meta = {
  title: 'Pages/Agents/Run/Rail',
  parameters: { layout: 'fullscreen' },
  // The run page provides tooltips; the grades' question text shows in one.
  decorators: [
    (Story) => (
      <TooltipProvider>
        <Story />
      </TooltipProvider>
    ),
  ],
};
export default meta;

// The finished run takes its own id, so its stream and record are told apart from the live one.
const done = doneRun();
done.body.data.id = 'done-run';

const STATES = [
  { label: 'Code Reviewer, mid-run', run: runningRun() },
  { label: 'Code Reviewer, finished', run: done },
  { label: 'Long session', run: slackRun() },
];

/** The rail as the page draws it, from the run's context. */
function Rail() {
  return <RunRail run={toRunView(useRunContext())} />;
}

export const States: StoryObj = {
  decorators: runStory(STATES.map((s) => s.run)),
  render: () => (
    <Stack style={{ padding: 32, background: 'var(--color-background)', minHeight: '100vh' }}>
      <Grid
        style={{
          gridTemplateColumns: `repeat(${STATES.length}, 320px)`,
          columnGap: 40,
          rowGap: 16,
          alignItems: 'start',
        }}
      >
        {STATES.map((s) => (
          <Heading key={s.label} variant="subsection">
            {s.label}
          </Heading>
        ))}
        {STATES.map((s) => (
          <RunProvider key={s.label} slug="code-reviewer" runId={s.run.body.data.id}>
            <Rail />
          </RunProvider>
        ))}
      </Grid>
    </Stack>
  ),
};

/** A finished run with its own id and `grades`. */
const graded = (key: keyof typeof RUN_GRADES): MockRun => {
  const run = doneRun();
  run.body.data.id = `graded-${key}`;
  run.grades = RUN_GRADES[key];
  return run;
};

const GRADE_STATES = [
  { label: 'Pass', run: graded('pass') },
  { label: 'Fail', run: graded('fail') },
  { label: 'Uncertain', run: graded('uncertain') },
  { label: 'Capped', run: graded('capped') },
  { label: 'Not applicable', run: graded('notApplicable') },
  { label: 'Too large', run: graded('tooLarge') },
  { label: 'Error', run: graded('error') },
  { label: 'Not graded yet', run: runningRun() },
];

/** The rail's Grades section in every state, each read through the run's grades route. */
export const Grades: StoryObj = {
  decorators: runStory(GRADE_STATES.map((s) => s.run)),
  render: () => (
    // Hades has no prop for the background, fixed column widths or cell alignment.
    <Stack padding="loose" minHeight="screen" style={{ background: 'var(--color-background)' }}>
      <Grid
        gapX="spacious"
        gapY="loose"
        style={{ gridTemplateColumns: 'repeat(4, 320px)', alignItems: 'start' }}
      >
        {GRADE_STATES.map((s) => (
          <Stack key={s.label} gap="snug">
            <Heading variant="subsection">{s.label}</Heading>
            <RunProvider slug="code-reviewer" runId={s.run.body.data.id}>
              <Rail />
            </RunProvider>
          </Stack>
        ))}
      </Grid>
    </Stack>
  ),
};
