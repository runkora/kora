import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { PrincipalBadge } from './principal-badge';

/**
 * PrincipalBadge
 *
 * A small badge that names what kind of identity something is: an agent, a person, an access token
 * or a platform service. Use it wherever a grant, budget or request needs to say who it belongs to.
 */
const meta: Meta<typeof PrincipalBadge> = {
  title: 'App/Atoms/PrincipalBadge',
  component: PrincipalBadge,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof PrincipalBadge>;

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="The four principal types">
        <Stack direction="horizontal" gap="snug" align="center">
          <PrincipalBadge type="AGENT" />
          <PrincipalBadge type="HUMAN" />
          <PrincipalBadge type="PAT" />
          <PrincipalBadge type="SERVICE" />
        </Stack>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="type">
        <Option name="AGENT" use="An agent acting as itself.">
          <PrincipalBadge type="AGENT" />
        </Option>
        <Option name="HUMAN" use="A person signed in through their organisation.">
          <PrincipalBadge type="HUMAN" />
        </Option>
        <Option name="PAT" use="A personal access token.">
          <PrincipalBadge type="PAT" />
        </Option>
        <Option name="SERVICE" use="A platform service. Any other value takes this colour too.">
          <PrincipalBadge type="SERVICE" />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
