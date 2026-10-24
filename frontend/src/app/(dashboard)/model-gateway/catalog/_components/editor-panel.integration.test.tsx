import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { EditorPanel } from './editor-panel';
import type { VirtualModel } from '../_types/catalog';

const MODEL: VirtualModel = {
  id: 'vm-1',
  family: 'anthropic',
  name: 'claude-sonnet-4-6',
  displayName: 'Claude Sonnet 4.6',
  description: '',
  strategy: 'priority',
  routes: [],
  enabled: true,
  declared: false,
};

function renderPanel(model: VirtualModel, apiAuthoring = true) {
  return renderWithProviders(
    <EditorPanel
      model={model}
      onClose={vi.fn()}
      onSaved={vi.fn()}
      onDeleted={vi.fn()}
      providerModels={[]}
      providers={[]}
      apiAuthoring={apiAuthoring}
    />,
  );
}

describe('EditorPanel managed in git', () => {
  it('offers save and delete on an API model', () => {
    renderPanel(MODEL);
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument();
  });

  it('shows a declared model read-only', () => {
    renderPanel({ ...MODEL, declared: true });
    expect(screen.getByText('Managed in git')).toBeInTheDocument();
    expect(screen.getByText(/item is managed in git/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Save Changes' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('e.g. claude-sonnet-4-6')).toBeDisabled();
    expect(screen.getByRole('button', { name: /add route/i })).toBeDisabled();
  });

  it('shows every model read-only with the platform switch off', () => {
    renderPanel(MODEL, false);
    expect(
      screen.getByText(/Virtual models on this platform are managed in git/),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Save Changes' })).not.toBeInTheDocument();
  });
});
