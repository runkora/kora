// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

import type { Meta, StoryObj } from '@storybook/react';
import { Grid, Heading, Stack, TooltipProvider } from '@runkora/hades';
import { RUN_GRADES } from '@runkora/app/(dashboard)/agents/_lib/grades-proposed';
import { doneRun, runStory } from '../_lib/run-page-mock-data';
import { RunShellProposed } from './run-shell-proposed';
import { RailGrades } from './run-grades-proposed';

/** Proposed: a finished run's grades in the run page rail, under Spend, one line per question. */
const meta: Meta<typeof RunShellProposed> = {
  title: 'Proposed/Graders/Run page',
  component: RunShellProposed,
  parameters: {
    layout: 'fullscreen',
    nextjs: {
      appDirectory: true,
      navigation: { pathname: '/agents/code-reviewer/runs/3f2c9a10-5b7e-4d21-9c8a-6e1f0b7d2a44' },
    },
  },
};

export default meta;
type Story = StoryObj<typeof RunShellProposed>;

const run = doneRun();
const args = { slug: run.body.agent_type.slug ?? 'code-reviewer', runId: run.body.data.id };

/** Grades in the rail; one question failed. */
export const RunWithGrades: Story = {
  name: 'Run with grades',
  args: { ...args, grades: RUN_GRADES.fail ?? [] },
  decorators: runStory([doneRun()]),
};

const STATES = [
  { label: 'Pass', key: 'pass' },
  { label: 'Fail', key: 'fail' },
  { label: 'Uncertain', key: 'uncertain' },
  { label: 'Capped', key: 'capped' },
  { label: 'Not applicable', key: 'notApplicable' },
  { label: 'Too large', key: 'tooLarge' },
  { label: 'Not graded yet', key: 'pending' },
];

/** The rail's Grades section in every grade state, side by side. */
export const GradeStates: StoryObj = {
  name: 'Grade states',
  render: () => (
    <TooltipProvider>
      <Stack style={{ padding: 32, background: 'var(--color-background)', minHeight: '100vh' }}>
        <Grid
          style={{
            gridTemplateColumns: `repeat(4, 276px)`,
            columnGap: 56,
            rowGap: 40,
            alignItems: 'start',
          }}
        >
          {STATES.map((s) => (
            <Stack key={s.key} gap="snug">
              <Heading variant="subsection">{s.label}</Heading>
              <Stack style={{ width: 276 }}>
                <RailGrades grades={RUN_GRADES[s.key] ?? []} />
              </Stack>
            </Stack>
          ))}
        </Grid>
      </Stack>
    </TooltipProvider>
  ),
};
