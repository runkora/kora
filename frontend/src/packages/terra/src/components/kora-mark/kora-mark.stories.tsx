import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { KoraMark } from './kora-mark';

/**
 * The Kora brand mark, drawn as one circle cut in half. Use it beside the product name.
 */
const meta: Meta<typeof KoraMark> = {
  title: 'Terra/Atoms/KoraMark',
  component: KoraMark,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof KoraMark>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Beside the product name">
        <div className="flex items-center gap-2">
          <KoraMark size={22} />
          <span className="text-sm font-semibold">Kora</span>
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option
          name="size"
          use="The height in pixels. The width follows the mark. The default is 20."
        >
          <div className="flex items-end gap-6">
            <KoraMark size={16} />
            <KoraMark size={32} />
            <KoraMark size={64} />
          </div>
        </Option>
        <Option
          name="title"
          use="Gives the mark an accessible name. Without it the mark is hidden from screen readers."
        >
          <KoraMark size={32} title="Kora" />
        </Option>
      </OptionTable>
    </div>
  ),
};
