import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { EnvironmentSizes, SandboxImage } from '@orbit/shared/api';
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
  { name: 'kora-base', description: 'Go and Python.', image: 'r/base:1', valid: true, warm: true },
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

const SIZES: EnvironmentSizes = {
  default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
};

function renderModal(
  environment: EnvironmentRow | null,
  images: SandboxImage[] | null = IMAGES,
  sizes: EnvironmentSizes | null = SIZES,
) {
  const onSaved = vi.fn();
  render(
    <EnvironmentFormModal
      open
      environment={environment}
      connections={[]}
      images={images}
      sizes={sizes}
      onClose={vi.fn()}
      onSaved={onSaved}
    />,
  );
  return { dialog: within(screen.getByRole('dialog')), onSaved };
}

const value = (dialog: ReturnType<typeof within>, label: string) =>
  (dialog.getByLabelText(label) as HTMLInputElement | HTMLSelectElement).value;

describe('EnvironmentFormModal', () => {
  beforeEach(() => {
    createEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
    updateEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
  });

  it('lists every registered image, kora-base included, and disables invalid ones', () => {
    const { dialog } = renderModal(null);
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    const options = [...select.options].map((o) => ({ label: o.text, disabled: o.disabled }));
    expect(options).toEqual([
      { label: 'acme-go', disabled: false },
      { label: 'acme-ops (not valid)', disabled: true },
      { label: 'kora-base', disabled: false },
    ]);
  });

  it('starts a new environment on kora-base at the default size, with each maximum shown', () => {
    const { dialog } = renderModal(null);
    expect(value(dialog, 'Sandbox image')).toBe('kora-base');
    expect(dialog.getByText('Go and Python.')).toBeInTheDocument();
    expect([value(dialog, 'CPU'), value(dialog, 'Memory'), value(dialog, 'Local disk')]).toEqual([
      '2',
      '4Gi',
      '10Gi',
    ]);
    expect(dialog.getByText('Maximum 8')).toBeInTheDocument();
    expect(dialog.getByText('Maximum 16Gi')).toBeInTheDocument();
    expect(dialog.getByText('Maximum 50Gi')).toBeInTheDocument();
  });

  it('sends kora-base explicitly when the image is left alone', async () => {
    const { dialog, onSaved } = renderModal(null);
    fireEvent.change(dialog.getByLabelText('Name'), { target: { value: 'Plain' } });
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        sandbox_image: 'kora-base',
        size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
      }),
    );
  });

  it('sends the chosen image and size on create', async () => {
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
        size: { cpu: '4', memory: '8Gi', disk: '10Gi' },
      }),
    );
  });

  it('blocks a size above the maximum before submitting', () => {
    const { dialog } = renderModal(null);
    fireEvent.change(dialog.getByLabelText('Name'), { target: { value: 'Big' } });
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: '64' } });
    expect(dialog.getByText('Above the maximum of 8.')).toBeInTheDocument();
    const create = dialog.getByRole('button', { name: 'Create environment' });
    expect(create).toBeDisabled();
    fireEvent.click(create);
    expect(createEnvironment).not.toHaveBeenCalled();

    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: '8' } });
    expect(create).toBeEnabled();
  });

  it('blocks saving an environment whose image is not valid', () => {
    const { dialog } = renderModal(ROW);
    expect(dialog.getByText('runs as root')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();

    fireEvent.change(dialog.getByLabelText('Sandbox image'), { target: { value: 'kora-base' } });
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeEnabled();
  });

  it('keeps the API error as the backstop when the limits did not load', async () => {
    updateEnvironment.mockResolvedValue({
      environment: null,
      error: 'invalid environment: size.cpu 64 is above the maximum 8',
    });
    const { dialog } = renderModal({ ...ROW, sandboxImage: 'kora-base' }, IMAGES, null);
    expect(dialog.getByText(/Could not load the size limits/)).toBeInTheDocument();
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: '64' } });
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    expect(
      await dialog.findByText('invalid environment: size.cpu 64 is above the maximum 8'),
    ).toBeInTheDocument();
  });

  it('keeps the current image selectable when the image list did not load', () => {
    const { dialog } = renderModal(null, null);
    expect(dialog.getByText('Could not load the registered images.')).toBeInTheDocument();
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    expect([...select.options].map((o) => [o.text, o.disabled])).toEqual([['kora-base', false]]);
  });
});
