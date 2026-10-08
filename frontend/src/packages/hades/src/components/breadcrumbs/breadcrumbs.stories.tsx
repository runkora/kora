import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Breadcrumbs } from './breadcrumbs';

/**
 * Breadcrumbs
 *
 * Shows where the current page sits, with a link back to each level above it.
 * The last item is the current page and is not a link.
 */
const meta: Meta<typeof Breadcrumbs> = {
  title: 'Hades/Molecules/Breadcrumbs',
  component: Breadcrumbs,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Breadcrumbs>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Three levels">
        <Breadcrumbs
          aria-label="Overview trail"
          items={[
            { label: 'Agents', href: '#' },
            { label: 'Support triage', href: '#' },
            { label: 'Runs' },
          ]}
        />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="items" use="The levels, root first. Each has a label and an optional link.">
          <Breadcrumbs
            aria-label="Three level trail"
            items={[
              { label: 'Policies', href: '#' },
              { label: 'Grants', href: '#' },
              { label: 'Deploy agent' },
            ]}
          />
        </Option>
        <Option
          name="Last item"
          use="Always the current page. It is bold, has no link, and is marked for screen readers."
        >
          <Breadcrumbs
            aria-label="Current page trail"
            items={[{ label: 'Agents', href: '#' }, { label: 'Support triage' }]}
          />
        </Option>
        <Option name="No href" use="A level without a link shows as plain text.">
          <Breadcrumbs
            aria-label="Plain text level trail"
            items={[{ label: 'Settings' }, { label: 'Members', href: '#' }, { label: 'Invite' }]}
          />
        </Option>
        <Option
          name="aria-label"
          use='Names the navigation for screen readers. Defaults to "Breadcrumb".'
        >
          <Breadcrumbs
            aria-label="Agent location"
            items={[{ label: 'Agents', href: '#' }, { label: 'Support triage' }]}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
