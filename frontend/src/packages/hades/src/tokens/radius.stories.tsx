import type { Meta, StoryObj } from '@storybook/react';
import { Group, Page, Tile, readRadius } from './foundation-ui';

/**
 * Radius
 *
 * Corner radii are multiples of `--hades-radius-unit`. Changing that one value
 * changes every radius.
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
    detail: '1x the unit. Badges and tags.',
  },
  {
    cssVar: '--radius-md',
    label: 'Medium',
    utility: 'rounded-md',
    detail: '2x the unit. Inputs and buttons.',
  },
  {
    cssVar: '--radius-lg',
    label: 'Large',
    utility: 'rounded-lg',
    detail: '3x the unit. Cards and panels.',
  },
  {
    cssVar: '--radius-xl',
    label: 'Extra large',
    utility: 'rounded-xl',
    detail: '3x the unit. Dialogs.',
  },
  {
    cssVar: '--radius-2xl',
    label: '2x large',
    utility: 'rounded-2xl',
    detail: '4x the unit. Chat input.',
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
