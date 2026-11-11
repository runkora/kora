import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@runkora/hades';
import { budgetRun, joinedRun } from '../_lib/run-page-proposed-mock-data';
import { RunMixPage } from './run-mix-proposed';

/**
 * The run page at device sizes, side by side. Each frame is the page at that width; the layout
 * follows the frame, so panes fold into a drawer and a sheet as they would on the device.
 */
const meta: Meta = {
  title: 'Pages/Agents/Run (mix)/Responsive',
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

const EARLIER = [
  { label: 'Review pull request #5 in example-org/stats-lib adding Mode', meta: '1d' },
  { label: 'Review pull request #4 fixing Mean on an empty slice', meta: '3d' },
];

function Frames({ run }: { run: ReturnType<typeof joinedRun> }) {
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
            <RunMixPage run={run} earlier={EARLIER} height={`${d.height}px`} />
          </Stack>
        </Stack>
      ))}
    </Stack>
  );
}

/** Mid-run, with a mention joined and a message waiting. */
export const Running: StoryObj = {
  render: () => (
    <Frames
      run={{
        ...joinedRun(),
        queued: [{ id: 'q1', text: 'Also check Median on an empty slice before you decide.' }],
      }}
    />
  ),
};

/** Stopped by a spent budget. */
export const BudgetExhausted: StoryObj = {
  render: () => <Frames run={budgetRun()} />,
};
