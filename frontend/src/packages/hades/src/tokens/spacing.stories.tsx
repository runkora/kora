import type { Meta, StoryObj } from '@storybook/react';
import { Text } from '../components/typography/typography';
import { Page, readWidth, useLive } from './foundation-ui';

/**
 * Spacing
 *
 * Gap sizes used by `Stack` and `Grid`. Each name is a Tailwind spacing step.
 * The pixel value is measured from the bar shown.
 */
const meta: Meta = {
  title: 'Hades/Foundation/Spacing',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

const STEPS = [
  { name: 'tight', utility: 'gap-1', bar: 'w-1' },
  { name: 'snug', utility: 'gap-2', bar: 'w-2' },
  { name: 'cozy', utility: 'gap-3', bar: 'w-3' },
  { name: 'comfortable', utility: 'gap-4', bar: 'w-4' },
  { name: 'relaxed', utility: 'gap-6', bar: 'w-6' },
  { name: 'loose', utility: 'gap-8', bar: 'w-8' },
  { name: 'spacious', utility: 'gap-10', bar: 'w-10' },
];

function Row({ name, utility, bar }: (typeof STEPS)[number]) {
  const [ref, value] = useLive(readWidth);
  return (
    <div className="grid grid-cols-[140px_90px_50px_1fr] items-center gap-4 border-b border-border-subtle py-3 last:border-0">
      <Text size="supporting" weight="emphasis">
        {name}
      </Text>
      <Text size="caption" mono color="subtle">
        {utility}
      </Text>
      <Text size="caption" mono color="muted">
        {value}
      </Text>
      <div>
        <div ref={ref} className={`h-3 rounded-sm bg-accent ${bar}`} />
      </div>
    </div>
  );
}

export const Scale: Story = {
  render: () => (
    <Page>
      <div className="max-w-2xl">
        {STEPS.map((s) => (
          <Row key={s.name} {...s} />
        ))}
      </div>
    </Page>
  ),
};
