import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { McpServerDetailShell } from './mcp-server-detail-shell';
import {
  GITHUB,
  GITHUB_TOOLS,
  KORA,
  KORA_TOOLS,
  MOCK_TOOLS,
  POSTGRES,
  SLACK,
  SLACK_TOOLS,
} from './mcp-mock-data';

/** One MCP server's detail: tools by domain, connect snippet and trust card. */
const meta: Meta<typeof McpServerDetailShell> = {
  title: 'Pages/MCP Servers/Detail',
  component: McpServerDetailShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway/github' } },
  },
  decorators: [dashboardPage()],
};

export default meta;
type Story = StoryObj<typeof McpServerDetailShell>;

/** Delegated identity with a large tool catalog across five domains. */
export const ManyTools: Story = {
  args: { serverSlug: 'github', server: GITHUB, tools: MOCK_TOOLS, error: null },
};

export const DelegatedIdentity: Story = {
  parameters: { nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway/kora' } } },
  args: { serverSlug: 'kora', server: KORA, tools: KORA_TOOLS, error: null },
};

/** A service-identity server: the gateway injects its own credential. */
export const Autonomous: Story = {
  parameters: { nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway/slack' } } },
  args: { serverSlug: 'slack', server: SLACK, tools: SLACK_TOOLS, error: null },
};

/** Stale health, a tenant-scoped server and no tools introspected yet. */
export const StaleNoTools: Story = {
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway/postgres-analytics' } },
  },
  args: {
    serverSlug: POSTGRES.name,
    server: { ...POSTGRES, tool_count: 0, domains: [] },
    tools: [],
    error: null,
  },
};

export const LoadError: Story = {
  args: {
    serverSlug: 'github',
    server: null,
    tools: GITHUB_TOOLS,
    error: 'Failed to load the MCP server',
  },
};

export const NotFound: Story = {
  parameters: { nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway/jira' } } },
  args: { serverSlug: 'jira', server: null, tools: [], error: null },
};
