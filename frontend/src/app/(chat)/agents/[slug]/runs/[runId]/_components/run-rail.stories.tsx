import type { Meta, StoryObj } from '@storybook/react';
import { Grid, Heading, Stack } from '@runkora/hades';
import { doneRun, runStory, runningRun, slackRun } from '../_lib/run-page-mock-data';
import { RunProvider, useRunContext } from '../_context/run-context';
import { toRunView } from '../_lib/to-run-view';
import { RunRail } from './run-rail';

/** The run page's right rail: the Code Reviewer mid-run and finished, and a long Slack session. */
const meta: Meta = {
  title: 'Pages/Agents/Run/Rail',
  parameters: { layout: 'fullscreen' },
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
