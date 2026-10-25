import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
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
});
