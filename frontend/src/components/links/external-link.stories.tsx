import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@runkora/hades';
import { Example, Option, OptionTable } from '../../packages/hades/src/docs/story-ui';
import { ExternalLink } from './external-link';

/**
 * ExternalLink
 *
 * A link to a site outside kora. It opens in a new tab and only accepts URLs from the approved
 * list.
 */
const meta: Meta<typeof ExternalLink> = {
  title: 'App/Atoms/ExternalLink',
  component: ExternalLink,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ExternalLink>;

const URL = 'https://github.com/clear-route/orbit';

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Inline link">
        <Text>
          The source is on <ExternalLink url={URL}>GitHub</ExternalLink>.
        </Text>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="size">
        <Option name="inherit" use="Takes the size of the surrounding text. The default.">
          <ExternalLink url={URL}>kora on GitHub</ExternalLink>
        </Option>
        <Option name="sm" use="Small text, for references beside other content.">
          <ExternalLink size="sm" url={URL}>
            kora on GitHub
          </ExternalLink>
        </Option>
        <Option name="md" use="Regular body text size.">
          <ExternalLink size="md" url={URL}>
            kora on GitHub
          </ExternalLink>
        </Option>
      </OptionTable>
    </Stack>
  ),
};
