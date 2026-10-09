import type { Meta, StoryObj } from '@storybook/react';
import { Block, Example } from '../../docs/story-ui';
import { Section } from './section';

/**
 * Section
 *
 * A block of page content with an optional heading and description. Stack
 * several to build a page with even spacing.
 */
const meta: Meta<typeof Section> = {
  title: 'Hades/Layout/Section',
  component: Section,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Section>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-2xl space-y-10">
      <Example title="Heading and description">
        <Section heading="Active agents" description="Agents running in this workspace.">
          <Block>Content</Block>
        </Section>
      </Example>
      <Example title="Heading only">
        <Section heading="Active agents">
          <Block>Content</Block>
        </Section>
      </Example>
      <Example title="Content only">
        <Section>
          <Block>Content</Block>
        </Section>
      </Example>
    </div>
  ),
};
