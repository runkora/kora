import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon/icon';
import { Text } from '../typography/typography';
import { Alert } from './alert';

/**
 * Alert
 *
 * A short inline message about an error, a warning or something the user should know.
 * Put an icon and a line of text inside it.
 */
const meta: Meta<typeof Alert> = {
  title: 'Hades/Atoms/Alert',
  component: Alert,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Alert>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Error">
        <Alert variant="error">
          <Icon name="alert-circle" size="xs" color="status-error" />
          <Text size="caption">Credential rotation failed. Try again.</Text>
        </Alert>
      </Example>
      <Example title="Warning">
        <Alert variant="warning">
          <Icon name="alert-circle" size="xs" color="status-degraded" />
          <Text size="caption">Weights must add up to 100%.</Text>
        </Alert>
      </Example>
      <Example title="Info">
        <Alert variant="info">
          <Icon name="info" size="xs" color="accent" />
          <Text size="caption">The gateway is processing your request.</Text>
        </Alert>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='variant="info"' use="A neutral notice. The default.">
          <Alert variant="info">
            <Icon name="info" size="xs" color="accent" />
            <Text size="caption">Changes apply to new runs only.</Text>
          </Alert>
        </Option>
        <Option name='variant="warning"' use="Something is off but not broken.">
          <Alert variant="warning">
            <Icon name="alert-circle" size="xs" color="status-degraded" />
            <Text size="caption">This key expires in 3 days.</Text>
          </Alert>
        </Option>
        <Option name='variant="error"' use="An action failed or an input is invalid.">
          <Alert variant="error">
            <Icon name="alert-circle" size="xs" color="status-error" />
            <Text size="caption">Deploy failed.</Text>
          </Alert>
        </Option>
        <Option
          name='role="status"'
          use="Announce the message politely instead of interrupting. The default role is alert."
        >
          <Alert variant="info" role="status">
            <Icon name="info" size="xs" color="accent" />
            <Text size="caption">Saved.</Text>
          </Alert>
        </Option>
      </OptionTable>
    </div>
  ),
};
