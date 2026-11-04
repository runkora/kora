import { beforeEach, describe, expect, it, vi } from 'vitest';
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
let dirty = true;
let saving = false;
const storedMcpTools = ['mcp:github:issues.list:use', 'mcp:github:pulls.get:use', 'mcp:slack:use'];

vi.mock('../_hooks/use-agent-configuration', () => ({
  useAgentConfiguration: () => ({
    agent: current,
    name: current.name,
    setName: vi.fn(),
    description: '',
    models: [],
    systemPrompt: 'Review every pull request.\nBe brief.',
    framework: 'claude-code',
    setFramework: vi.fn(),
    modelId: 'anthropic/claude-sonnet-4-6',
    mcpTools: storedMcpTools,
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
    isDirty: dirty,
    saving,
    handleSave: vi.fn(),
    handleDiscard: vi.fn(),
    deleteOpen: false,
    setDeleteOpen: vi.fn(),
    handleDelete: vi.fn(),
  }),
}));

import { AgentConfigureModal } from './agent-configure-modal';

function renderModal(
  props: { initialSection?: string; onClose?: () => void } = {},
  options?: Parameters<typeof renderWithProviders>[1],
) {
  return renderWithProviders(
    <AgentConfigureModal
      open
      onClose={props.onClose ?? vi.fn()}
      initialSection={props.initialSection}
    />,
    options,
  );
}

describe('AgentConfigureModal', () => {
  beforeEach(() => {
    dirty = true;
    saving = false;
  });

  it('renders nothing while closed', () => {
    renderWithProviders(<AgentConfigureModal open={false} onClose={vi.fn()} />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('asks before closing with unsaved changes, and discards on confirm', () => {
    const onClose = vi.fn();
    renderModal({ onClose });
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' }).at(-1)!);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes without asking when nothing changed', () => {
    dirty = false;
    const onClose = vi.fn();
    renderModal({ onClose });
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' }).at(-1)!);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });

  it('lets an editor change an agent built in the dashboard', () => {
    current = agent;
    renderWithProviders(<AgentConfigureModal open onClose={vi.fn()} />);
    expect(screen.getByDisplayValue('Reviewer')).toBeEnabled();
  });

  it('keeps every field read only for a declared agent', () => {
    current = { ...agent, declared: true };
    renderWithProviders(<AgentConfigureModal open onClose={vi.fn()} />);
    expect(screen.getByDisplayValue('Reviewer')).toBeDisabled();
    // The banner and header pill say so once; there is nothing to save.
    expect(screen.getByText('Managed in git')).toBeInTheDocument();
    expect(screen.getByText(/This is a read-only config view/)).toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Discard' })).toBeNull();
  });

  it('has no Danger zone for an agent managed in git', () => {
    current = { ...agent, declared: true };
    renderModal();
    expect(screen.queryByText('Danger zone')).toBeNull();
  });

  it('offers a Danger zone to an editor', () => {
    current = { ...agent, canDelete: true } as typeof agent;
    renderModal();
    fireEvent.click(screen.getByRole('button', { name: /Danger zone/ }));
    expect(screen.getByRole('button', { name: 'Delete agent' })).toBeInTheDocument();
  });

  it('marks the slug and identity model read-only and disabled', () => {
    current = agent;
    renderModal();
    expect(screen.getAllByText('Read-only')).toHaveLength(2);
    expect(screen.getByLabelText('Slug (immutable)')).toBeDisabled();
    expect(screen.getByLabelText('Identity model (immutable)')).toBeDisabled();
  });

  it('keeps every field read only in a locked tenant', () => {
    current = agent;
    renderWithProviders(<AgentConfigureModal open onClose={vi.fn()} />, { apiAuthoring: false });
    expect(screen.getByDisplayValue('Reviewer')).toBeDisabled();
  });

  it('dims the behaviour controls for a viewer who cannot edit', () => {
    current = { ...agent, canEdit: false };
    renderModal({ initialSection: 'behaviour' });
    expect(screen.getByDisplayValue(/Review every pull request\./)).toBeDisabled();
    for (const radio of screen.getAllByRole('radio')) expect(radio).toBeDisabled();
    expect(screen.queryByText(/enabled by your platform/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  });

  it('dims the tools and connections for a viewer who cannot edit', () => {
    current = { ...agent, canEdit: false };
    renderModal({ initialSection: 'tools' });
    expect(screen.getByText('3')).toBeInTheDocument();
    for (const box of screen.queryAllByRole('checkbox')) expect(box).toBeDisabled();
  });

  it('does not close while a save is in flight', () => {
    saving = true;
    const onClose = vi.fn();
    renderModal({ onClose });
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' }).at(-1)!);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });

  it("shows the stored model when it is not in the viewer's catalogue", () => {
    current = { ...agent, canEdit: false };
    renderModal({ initialSection: 'behaviour' });
    expect(screen.getAllByText('anthropic/claude-sonnet-4-6').length).toBeGreaterThan(0);
  });

  it('shows server-wide and uncatalogued grants to a read-only viewer', () => {
    current = { ...agent, canEdit: false };
    renderModal({ initialSection: 'tools' });
    fireEvent.click(screen.getByRole('button', { name: /Tools & access/ }));
    expect(screen.getByText('slack')).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('jira')).toBeInTheDocument();
  });
});
