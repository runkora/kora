import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/terra';
import type { EffectiveAccess } from '@orbit/shared/api';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { EffectiveAccessPanel } from './effective-access-panel';

/**
 * EffectiveAccessPanel
 *
 * Shows every host an agent can reach, merged from its own allowlist, its environment and its connections. Each host is tagged with where it came from.
 */
const meta: Meta<typeof EffectiveAccessPanel> = {
  title: 'App/Organisms/EffectiveAccessPanel',
  component: EffectiveAccessPanel,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof EffectiveAccessPanel>;

const access: EffectiveAccess = {
  open: false,
  own_hosts: ['api.example.com', '*.staging.internal'],
  environment_id: 'env-1',
  environment_name: 'Staging',
  environment_hosts: ['telemetry.orbit.sh'],
  connector_egress: [
    {
      connection: 'github',
      adapter: 'github_app',
      hosts: ['github.com', 'api.github.com', 'codeload.github.com', '*.githubusercontent.com'],
    },
  ],
  connectors: [{ connection: 'github', actions: ['repo:read'] }],
  hosts: [
    '*.githubusercontent.com',
    '*.staging.internal',
    'api.example.com',
    'api.github.com',
    'codeload.github.com',
    'github.com',
    'telemetry.orbit.sh',
  ],
};

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Resolved access">
        <EffectiveAccessPanel access={access} loading={false} error={null} />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option
          name="access"
          use="A resolved list of hosts. Hover a source to highlight its hosts."
        >
          <EffectiveAccessPanel access={access} loading={false} error={null} />
        </Option>
        <Option name="access.open" use="Egress is unrestricted, so no host list applies.">
          <EffectiveAccessPanel
            access={{ ...access, open: true, hosts: [], own_hosts: [] }}
            loading={false}
            error={null}
          />
        </Option>
        <Option name="loading" use="The preview is still resolving.">
          <EffectiveAccessPanel access={null} loading error={null} />
        </Option>
        <Option name="error" use="The preview failed. The message replaces the host list.">
          <EffectiveAccessPanel
            access={null}
            loading={false}
            error="Preview failed: policy service unreachable."
          />
        </Option>
        <Option name="description" use="Replaces the subtitle for non-agent uses.">
          <EffectiveAccessPanel
            access={access}
            loading={false}
            error={null}
            description="What this environment lets its agents reach."
          />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
