import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { KoraLogo } from './kora-logo';

/**
 * The Kora logo: the mark and the wordmark drawn together. Use it where the product
 * name appears as a logo, such as the sidebar header.
 */
const meta: Meta<typeof KoraLogo> = {
  title: 'Terra/Atoms/KoraLogo',
  component: KoraLogo,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof KoraLogo>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Sidebar size">
        <KoraLogo size={28} title="kora" />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="size" use="The height in pixels. The width follows. The default is 28.">
          <div className="flex items-end gap-8">
            <KoraLogo size={20} />
            <KoraLogo size={32} />
            <KoraLogo size={64} />
          </div>
        </Option>
        <Option
          name="title"
          use="Gives the logo an accessible name. Without it the logo is hidden from screen readers."
        >
          <KoraLogo size={32} title="kora" />
        </Option>
      </OptionTable>
    </div>
  ),
};
