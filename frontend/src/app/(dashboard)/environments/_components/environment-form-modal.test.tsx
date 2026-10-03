import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { SandboxImage } from '@orbit/shared/api';
import { EnvironmentFormModal } from './environment-form-modal';
import type { EnvironmentRow } from '../_types';

const createEnvironment = vi.fn();
const updateEnvironment = vi.fn();

vi.mock('../_services/environments-api', () => ({
  createEnvironment: (...args: unknown[]) => createEnvironment(...args),
  updateEnvironment: (...args: unknown[]) => updateEnvironment(...args),
  previewEnvironmentAccess: () => new Promise(() => {}),
}));

vi.mock('@orbit/components', () => ({
  ConnectionCards: () => null,
  EffectiveAccessPanel: () => null,
}));

const IMAGES: SandboxImage[] = [
  { name: 'acme-go', description: 'Go and buf.', image: 'r/go:1', valid: true, warm: true },
  {
    name: 'acme-ops',
    description: '',
    image: 'r/ops:1',
    valid: false,
    problem: 'runs as root',
    warm: false,
  },
  { name: 'kora-base', description: '', image: 'r/base:1', valid: true, warm: true },
];

const ROW: EnvironmentRow = {
  id: 'env-1',
  name: 'Ops',
  description: '',
  sandboxImage: 'acme-ops',
  size: { cpu: '1', memory: '2Gi', disk: '5Gi' },
  egressHosts: [],
  connectors: [],
  createdAt: '',
  updatedAt: '',
};

function renderModal(environment: EnvironmentRow | null, images: SandboxImage[] | null = IMAGES) {
  const onSaved = vi.fn();
  render(
    <EnvironmentFormModal
      open
      environment={environment}
      connections={[]}
      images={images}
      onClose={vi.fn()}
      onSaved={onSaved}
    />,
  );
  return { dialog: within(screen.getByRole('dialog')), onSaved };
}

describe('EnvironmentFormModal', () => {
  beforeEach(() => {
    createEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
    updateEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
  });

  it('offers the base image first and only valid images as choices', () => {
    const { dialog } = renderModal(null);
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    const options = [...select.options].map((o) => ({ label: o.text, disabled: o.disabled }));
    expect(options).toEqual([
      { label: 'kora base image', disabled: false },
      { label: 'acme-go', disabled: false },
      { label: 'acme-ops (not valid)', disabled: true },
    ]);
  });

  it('sends the image and size on create', async () => {
    const { dialog, onSaved } = renderModal(null);
    fireEvent.change(dialog.getByLabelText('Name'), { target: { value: 'Go services' } });
    fireEvent.change(dialog.getByLabelText('Sandbox image'), { target: { value: 'acme-go' } });
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: ' 4 ' } });
    fireEvent.change(dialog.getByLabelText('Memory'), { target: { value: '8Gi' } });
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'Go services',
        sandbox_image: 'acme-go',
        size: { cpu: '4', memory: '8Gi', disk: '' },
      }),
    );
  });

  it('blocks saving an environment whose image is not valid', () => {
    const { dialog } = renderModal(ROW);
    expect(dialog.getByText('runs as root')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();

    fireEvent.change(dialog.getByLabelText('Sandbox image'), { target: { value: '' } });
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeEnabled();
  });

  it('shows the API error when the size is above the platform maximum', async () => {
    updateEnvironment.mockResolvedValue({
      environment: null,
      error: 'invalid environment: size.cpu 64 is above the maximum 8',
    });
    const { dialog } = renderModal({ ...ROW, sandboxImage: '' });
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: '64' } });
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    expect(
      await dialog.findByText('invalid environment: size.cpu 64 is above the maximum 8'),
    ).toBeInTheDocument();
  });

  it('keeps the base image available when the image list did not load', () => {
    const { dialog } = renderModal(null, null);
    expect(dialog.getByText(/Could not load the registered images/)).toBeInTheDocument();
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    expect([...select.options].map((o) => o.text)).toEqual(['kora base image']);
  });
});
