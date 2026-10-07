import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography/typography';
import { Callout } from './callout';

/**
 * Callout
 *
 * A tinted box for a note that sits inside page content, such as a tip or a one-time warning.
 * For a failed action, use Alert.
 */
const meta: Meta<typeof Callout> = {
  title: 'Terra/Atoms/Callout',
  component: Callout,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Callout>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Explaining a feature">
        <Callout>
          <Text size="caption" color="muted">
            Grants apply to a person or agent directly, however they sign in.
          </Text>
        </Callout>
      </Example>
      <Example title="A one-time warning">
        <Callout variant="warning">
          <Text size="caption">Copy this key now. It will not be shown again.</Text>
        </Callout>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='variant="info"' use="Neutral guidance. The default.">
          <Callout variant="info">
            <Text size="caption">Roles are shared by everyone in the team.</Text>
          </Callout>
        </Option>
        <Option name='variant="warning"' use="Something the reader must not miss.">
          <Callout variant="warning">
            <Text size="caption">Deleting this agent also deletes its runs.</Text>
          </Callout>
        </Option>
        <Option name='variant="success"' use="A good result or a confirmation.">
          <Callout variant="success">
            <Text size="caption">Connection verified.</Text>
          </Callout>
        </Option>
        <Option name='variant="error"' use="A failure or a denied request.">
          <Callout variant="error">
            <Text size="caption">Access denied by policy.</Text>
          </Callout>
        </Option>
      </OptionTable>
    </div>
  ),
};
