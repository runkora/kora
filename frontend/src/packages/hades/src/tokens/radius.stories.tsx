import type { Meta, StoryObj } from '@storybook/react';
import { Group, Page, Tile, readRadius } from './foundation-ui';

/**
 * Radius
 *
 * Corner radii are five literal steps, each a settable theme token
 * (`--hades-raw-radius-sm` to `--hades-raw-radius-2xl`).
 */
const meta: Meta = {
  title: 'Hades/Foundation/Radius',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

const STEPS = [
  {
    cssVar: '--radius-sm',
    label: 'Small',
    utility: 'rounded-sm',
    detail: '6px by default. Badges and tags.',
  },
  {
    cssVar: '--radius-md',
    label: 'Medium',
    utility: 'rounded-md',
    detail: '12px by default. Inputs and buttons.',
  },
  {
    cssVar: '--radius-lg',
    label: 'Large',
    utility: 'rounded-lg',
    detail: '18px by default. Cards and panels.',
  },
  {
    cssVar: '--radius-xl',
    label: 'Extra large',
    utility: 'rounded-xl',
    detail: '18px by default. Dialogs.',
  },
  {
    cssVar: '--radius-2xl',
    label: '2x large',
    utility: 'rounded-2xl',
    detail: '24px by default. Chat input.',
  },
];

export const Scale: Story = {
  render: () => (
    <Page>
      <Group title="Radius scale" min={200}>
        {STEPS.map((s) => (
          <Tile
            key={s.cssVar}
            {...s}
            read={readRadius}
            preview={(ref) => (
              <div className="flex h-28 items-center justify-center bg-surface-elevated">
                <div
                  ref={ref}
                  className="h-14 w-14 border border-accent-border bg-accent-muted"
                  style={{ borderRadius: `var(${s.cssVar})` }}
                />
              </div>
            )}
          />
        ))}
      </Group>
    </Page>
  ),
};
