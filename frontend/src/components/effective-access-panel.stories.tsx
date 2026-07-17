import type { Meta, StoryObj } from '@storybook/react';
import type { EffectiveAccess } from '@orbit/shared/api';
import { EffectiveAccessPanel } from './effective-access-panel';

const meta: Meta<typeof EffectiveAccessPanel> = {
  title: 'App/Shared/EffectiveAccessPanel',
  component: EffectiveAccessPanel,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
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

export const Resolved: Story = {
  args: { access, loading: false, error: null },
};

export const OpenEgress: Story = {
  args: {
    access: { ...access, open: true, hosts: [], own_hosts: [] },
    loading: false,
    error: null,
  },
};

export const Loading: Story = {
  args: { access: null, loading: true, error: null },
};

export const Failed: Story = {
  args: { access: null, loading: false, error: 'Preview failed: policy service unreachable.' },
};
