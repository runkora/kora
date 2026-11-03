import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { mockApi } from '@runkora/storybook/mock-api';
import { ConsentFlow, type ConsentFlowProps } from './consent-flow';

/**
 * The MCP consent screen. The page renders it from server data, so the stories pass that data as
 * props. The decision POST and tenant discovery go through the mock API; discovery answers 404, so
 * choosing a tenant shows the switcher's error instead of leaving Storybook.
 */
const meta: Meta<typeof ConsentFlow> = {
  title: 'Pages/MCP consent',
  component: ConsentFlow,
  parameters: { layout: 'fullscreen', nextjs: { appDirectory: true } },
  decorators: [
    mockApi({
      'POST /api/mcp/authorize': { redirect: '#authorised' },
      'GET /api/auth/discover': () =>
        new Response(JSON.stringify({ code: 'not_found' }), { status: 404 }),
    }),
    (Story) => (
      <Stack align="center" justify="center" minHeight="screen" padding="comfortable">
        <Story />
      </Stack>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ConsentFlow>;

const base: ConsentFlowProps = {
  clientName: 'Claude Code',
  userEmail: 'dev@local.runkora.io',
  tenantName: 'Example Platform',
  tenants: [],
  consentPath: '/mcp/authorize?response_type=code&client_id=example',
  server: { name: 'kora', displayName: 'kora', endpoint: 'https://api.local.runkora.io/mcp/kora' },
  domains: [
    {
      name: 'agents',
      scope: 'mcp:kora:agents:use',
      tools: [
        { name: 'agents.list', scope: 'mcp:kora:agents.list:use', granted: true },
        { name: 'agents.run', scope: 'mcp:kora:agents.run:use', granted: false },
      ],
    },
  ],
  grantedCount: 1,
  totalCount: 2,
  techRows: [
    { k: 'response_type', v: 'code' },
    { k: 'client_id', v: 'example' },
    { k: 'redirect_uri', v: 'http://127.0.0.1:9876/callback', accent: true },
  ],
  actionUrl: '/api/mcp/authorize?response_type=code&client_id=example',
  csrfToken: 'example',
};

/** One tenant: the row names it and offers no switch. */
export const OneTenant: Story = {
  args: {
    ...base,
    tenants: [{ id: 'kora-tenant-example', displayName: 'Example Platform', active: true }],
  },
};

/** Several tenants: the row adds a Switch control. */
export const SeveralTenants: Story = {
  args: {
    ...base,
    tenants: [
      { id: 'kora-tenant-example', displayName: 'Example Platform', active: true },
      { id: 'kora-tenant-example-data', displayName: 'Example Data', active: false },
      { id: 'kora-tenant-example-ml', displayName: 'Example Machine Learning', active: false },
    ],
  },
};
