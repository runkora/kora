import type { Meta, StoryObj } from '@storybook/react';
import { Grid, Heading, Stack } from '@runkora/hades';
import { doneRun, joinedRun, longSession } from '../_lib/run-page-proposed-mock-data';
import { RunRail } from './run-rail-proposed';

/** The run page's right rail: the Code Reviewer mid-run and finished, and a long session. */
const meta: Meta = {
  title: 'Pages/Agents/Run (mix)/Rail',
  parameters: { layout: 'fullscreen' },
};
export default meta;

const STATES = [
  { label: 'Code Reviewer, mid-run', run: joinedRun() },
  { label: 'Code Reviewer, finished', run: doneRun() },
  { label: 'Long session', run: longSession() },
];

export const States: StoryObj = {
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
          <RunRail key={s.label} run={s.run} />
        ))}
      </Grid>
    </Stack>
  ),
};
