import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { BrandLogo } from './brand-logo';

// Placeholder artwork, so the story carries no product identity. Light ink for
// dark mode, dark ink for light mode.
const svg = (fill: string) =>
  `data:image/svg+xml;utf8,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 40" width="160" height="40"><rect x="2" y="2" width="36" height="36" rx="18" fill="${fill}"/><rect x="48" y="12" width="110" height="16" rx="8" fill="${fill}"/></svg>`,
  )}`;
const DARK = svg('#ece9e2');
const LIGHT = svg('#0d0f11');

/**
 * The identity slot for the logo. The app passes in one source per mode, and
 * the component shows the one for the current `data-theme`.
 */
const meta: Meta<typeof BrandLogo> = {
  title: 'Hades/Atoms/BrandLogo',
  component: BrandLogo,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof BrandLogo>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Sidebar size">
        <BrandLogo dark={DARK} light={LIGHT} size={30} title="Example" />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <OptionTable title="Props">
      <Option name="size" use="Height in pixels; the width follows the artwork">
        <BrandLogo dark={DARK} light={LIGHT} size={20} title="Example" />
        <BrandLogo dark={DARK} light={LIGHT} size={40} title="Example" />
      </Option>
      <Option
        name="title"
        use="Accessible name; without it the logo is decorative and hidden from assistive technology"
      >
        <BrandLogo dark={DARK} light={LIGHT} size={28} />
      </Option>
    </OptionTable>
  ),
};
