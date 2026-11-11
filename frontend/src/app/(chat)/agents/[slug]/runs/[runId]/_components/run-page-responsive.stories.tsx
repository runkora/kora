import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@runkora/hades';
import { budgetRun, runStory, runningRun, type MockRun } from '../_lib/run-page-mock-data';
import { RunProvider } from '../_context/run-context';
import { RunPage } from './run-page';

/**
 * The run page at device sizes, side by side. Each frame is the page at that width; the layout
 * follows the frame, so panes fold into a drawer and a sheet as they would on the device.
 */
const meta: Meta = {
  title: 'Pages/Agents/Run/Responsive',
  parameters: {
    layout: 'fullscreen',
    nextjs: {
      appDirectory: true,
      navigation: { pathname: '/agents/code-reviewer/runs/3f2c9a10-5b7e-4d21-9c8a-6e1f0b7d2a44' },
    },
  },
};
export default meta;

const DEVICES = [
  { label: 'iPad landscape · 1180', width: 1180, height: 820 },
  { label: 'iPad portrait · 820', width: 820, height: 1180 },
  { label: 'iPhone · 390', width: 390, height: 844 },
];

function Frames({ run }: { run: MockRun }) {
  return (
    <Stack
      direction="horizontal"
      align="start"
      style={{ gap: 40, padding: 32, background: 'var(--color-surface)', width: 'max-content' }}
    >
      {DEVICES.map((d) => (
        <Stack key={d.label} gap="snug">
          <Text size="supporting" color="muted">
            {d.label}
          </Text>
          <Stack
            style={{
              width: d.width,
              height: d.height,
              borderRadius: 18,
              overflow: 'hidden',
              border: '1px solid var(--color-border)',
            }}
          >
            <RunProvider slug="code-reviewer" runId={run.body.data.id}>
              <RunPage height={`${d.height}px`} />
            </RunProvider>
          </Stack>
        </Stack>
      ))}
    </Stack>
  );
}

/** Mid-run, with a mention joined. */
export const Running: StoryObj = {
  render: () => <Frames run={runningRun()} />,
  decorators: runStory([runningRun()]),
};

/** Stopped by a spent budget. */
export const BudgetExhausted: StoryObj = {
  render: () => <Frames run={budgetRun()} />,
  decorators: runStory([budgetRun()]),
};
