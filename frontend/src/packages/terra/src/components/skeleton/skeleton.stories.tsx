import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Skeleton } from './skeleton';

/**
 * Skeleton
 *
 * A pulsing grey placeholder that holds the shape of content while it loads.
 * Build one out of several skeletons that match the real layout.
 */
const meta: Meta<typeof Skeleton> = {
  title: 'Terra/Atoms/Skeleton',
  component: Skeleton,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Skeleton>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Card">
        <div className="flex flex-col gap-3 rounded-lg bg-surface p-4">
          <Skeleton width="three-quarters" />
          <Skeleton size="xs" />
          <Skeleton size="xs" />
          <Skeleton size="xs" width="two-thirds" />
        </div>
      </Example>
      <Example title="List row">
        <div className="flex items-center gap-3">
          <Skeleton shape="circle" size="md" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton size="xs" width="third" />
            <Skeleton size="xs" width="half" />
          </div>
          <Skeleton shape="block" size="xs" width="quarter" />
        </div>
      </Example>
      <Example title="Profile header">
        <div className="flex items-start gap-4">
          <Skeleton shape="circle" size="xl" />
          <div className="flex flex-1 flex-col gap-2 pt-1">
            <Skeleton width="half" />
            <Skeleton size="xs" width="three-quarters" />
          </div>
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='shape="line"' use="A row of text. The default.">
          <Skeleton shape="line" width="half" />
        </Option>
        <Option name='shape="block"' use="A rectangle, such as an image or a button.">
          <Skeleton shape="block" width="half" size="lg" />
        </Option>
        <Option
          name='shape="circle"'
          use="An avatar or icon. The width is ignored; size sets the diameter."
        >
          <Skeleton shape="circle" size="lg" />
        </Option>
        <Option
          name="size"
          use="Height, from xs (12 px) up to xl (64 px). Default sm. Also the diameter of a circle."
        >
          <div className="space-y-2">
            {(['xs', 'sm', 'md', 'lg', 'xl'] as const).map((size) => (
              <Skeleton key={size} size={size} width="half" />
            ))}
          </div>
        </Option>
        <Option name="size on a circle" use="The same five sizes as a row of circles.">
          <div className="flex items-end gap-3">
            {(['xs', 'sm', 'md', 'lg', 'xl'] as const).map((size) => (
              <Skeleton key={size} shape="circle" size={size} />
            ))}
          </div>
        </Option>
        <Option
          name="width"
          use="Share of the parent: quarter, third, half, two-thirds, three-quarters or full. Default full."
        >
          <div className="space-y-2">
            {(['quarter', 'third', 'half', 'two-thirds', 'three-quarters', 'full'] as const).map(
              (width) => (
                <Skeleton key={width} width={width} />
              ),
            )}
          </div>
        </Option>
      </OptionTable>
    </div>
  ),
};
