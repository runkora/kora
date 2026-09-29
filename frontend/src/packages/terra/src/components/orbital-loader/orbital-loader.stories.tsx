import type { Meta, StoryObj } from '@storybook/react';
import { Block, Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { OrbitalLoader } from './index';

/**
 * OrbitalLoader
 *
 * The Orbit loading animation. Use it while a page or panel waits for data.
 * It announces "Loading" to screen readers.
 */
const meta: Meta<typeof OrbitalLoader> = {
  title: 'Terra/Atoms/OrbitalLoader',
  component: OrbitalLoader,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof OrbitalLoader>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Default">
        <OrbitalLoader />
      </Example>
      <Example title="Inside a panel while data loads">
        <div className="flex h-40 items-center justify-center rounded-md border border-border-subtle bg-surface-elevated">
          <OrbitalLoader />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='size="md"' use="44 pixels square. The default, for a page or a panel.">
          <OrbitalLoader size="md" />
        </Option>
        <Option name='size="sm"' use="32 pixels square, for a button row or a small card.">
          <OrbitalLoader size="sm" />
        </Option>
        <Option name="className" use="Adds spacing or placement from outside, such as a margin.">
          <Stack direction="horizontal" gap="comfortable" align="center">
            <Block>Left</Block>
            <OrbitalLoader className="mx-6" />
            <Block>Right</Block>
          </Stack>
        </Option>
      </OptionTable>
    </div>
  ),
};
