import { describe, expect, it, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

const agent = {
  name: 'Reviewer',
  slug: 'reviewer',
  identityMode: 'autonomous',
  updatedAt: '2026-10-25T00:00:00Z',
  canEdit: true,
  declared: false,
};
let current = agent;

vi.mock('../_hooks/use-agent-configuration', () => ({
  useAgentConfiguration: () => ({
    agent: current,
    name: current.name,
    setName: vi.fn(),
    description: '',
    models: [],
    modelId: 'anthropic/claude-sonnet-4-6',
    mcpTools: ['mcp:github:issues.list:use', 'mcp:github:pulls.get:use'],
    mcpServers: [],
    mcpCatalog: [],
    availableConnections: [],
    connectors: [{ connection: 'jira', actions: ['read'] }],
    setDescription: vi.fn(),
    budgetLimitUSD: '',
    budgetCadence: 'monthly',
    budgetPerRunUSD: '',
    maxConcurrentRuns: '10',
    loopGuardrails: {},
    isDirty: true,
    handleSave: vi.fn(),
    handleDiscard: vi.fn(),
    deleteOpen: false,
    setDeleteOpen: vi.fn(),
    handleDelete: vi.fn(),
  }),
}));

import { AgentConfigurationTab } from './agent-configuration-tab';

describe('AgentConfigurationTab', () => {
  it('lets an editor change an agent built in the dashboard', () => {
    current = agent;
    renderWithProviders(<AgentConfigurationTab />);
    expect(screen.getByDisplayValue('Reviewer')).toBeEnabled();
  });

  it('keeps every field read only for a declared agent', () => {
    current = { ...agent, declared: true };
    renderWithProviders(<AgentConfigurationTab />);
    expect(screen.getByDisplayValue('Reviewer')).toBeDisabled();
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull();
  });

  it('keeps every field read only in a locked tenant', () => {
    current = agent;
    renderWithProviders(<AgentConfigurationTab />, { apiAuthoring: false });
    expect(screen.getByDisplayValue('Reviewer')).toBeDisabled();
  });

  it("shows a read-only viewer the agent's model, tools and connections", () => {
    current = { ...agent, canEdit: false };
    renderWithProviders(<AgentConfigurationTab initialSection="behaviour" />);
    expect(screen.getByText('anthropic/claude-sonnet-4-6')).toBeInTheDocument();
    expect(screen.queryByText(/enabled by your platform/)).toBeNull();
    fireEvent.click(screen.getByText('Tools & access'));
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('issues.list')).toBeInTheDocument();
    expect(screen.getByText('jira')).toBeInTheDocument();
    expect(screen.getByText('read')).toBeInTheDocument();
    expect(screen.queryByRole('checkbox')).toBeNull();
  });
});
