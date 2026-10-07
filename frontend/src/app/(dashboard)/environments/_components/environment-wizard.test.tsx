import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { EnvironmentSizes, SandboxImage } from '@orbit/shared/api';
import { EnvironmentWizard, stepForError } from './environment-wizard';
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

function renderWizard(
  environment: EnvironmentRow | null,
  images: SandboxImage[] | null = IMAGES,
  sizes: EnvironmentSizes | null = SIZES,
) {
  const onSaved = vi.fn();
  render(
    <EnvironmentWizard
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

type Dialog = ReturnType<typeof within>;

const value = (dialog: Dialog, label: string) =>
  (dialog.getByLabelText(label) as HTMLInputElement | HTMLSelectElement).value;

const next = (dialog: Dialog) => fireEvent.click(dialog.getByRole('button', { name: 'Continue' }));

// The desktop rail's step buttons carry the step title; the mobile rail's carry only the number.
const goToStep = (dialog: Dialog, title: string) =>
  fireEvent.click(dialog.getByRole('button', { name: new RegExp(title) }));

/** Names a new environment and moves to the sandbox step. */
function toSandbox(dialog: Dialog, name = 'Plain') {
  fireEvent.change(dialog.getByLabelText('Name'), { target: { value: name } });
  next(dialog);
}

describe('EnvironmentWizard', () => {
  beforeEach(() => {
    createEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
    updateEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
  });

  it('needs a name before leaving the basics step', () => {
    const { dialog } = renderWizard(null);
    expect(dialog.getByText('Enter a name before continuing.')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Continue' })).toBeDisabled();
    fireEvent.change(dialog.getByLabelText('Name'), { target: { value: 'Plain' } });
    expect(dialog.getByRole('button', { name: 'Continue' })).toBeEnabled();
  });

  it('lists every registered image, kora-base included, and disables invalid ones', () => {
    const { dialog } = renderWizard(null);
    toSandbox(dialog);
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    const options = [...select.options].map((o) => ({ label: o.text, disabled: o.disabled }));
    expect(options).toEqual([
      { label: 'acme-go', disabled: false },
      { label: 'acme-ops (not valid)', disabled: true },
      { label: 'kora-base', disabled: false },
    ]);
  });

  it('starts a new environment on kora-base at the default size, with each maximum shown', () => {
    const { dialog } = renderWizard(null);
    toSandbox(dialog);
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
    const { dialog, onSaved } = renderWizard(null);
    toSandbox(dialog);
    next(dialog);
    next(dialog);
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
    const { dialog, onSaved } = renderWizard(null);
    toSandbox(dialog, 'Go services');
    fireEvent.change(dialog.getByLabelText('Sandbox image'), { target: { value: 'acme-go' } });
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: ' 4 ' } });
    fireEvent.change(dialog.getByLabelText('Memory'), { target: { value: '8Gi' } });
    next(dialog);
    next(dialog);
    expect(dialog.getByText('acme-go')).toBeInTheDocument();
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

  it('prefills an existing environment and saves it', async () => {
    const { dialog, onSaved } = renderWizard({ ...ROW, sandboxImage: 'kora-base' });
    expect(value(dialog, 'Name')).toBe('Ops');
    goToStep(dialog, 'Sandbox');
    expect([value(dialog, 'CPU'), value(dialog, 'Memory'), value(dialog, 'Local disk')]).toEqual([
      '1',
      '2Gi',
      '5Gi',
    ]);
    goToStep(dialog, 'Review');
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(updateEnvironment).toHaveBeenCalledWith(
      'env-1',
      expect.objectContaining({ name: 'Ops', size: { cpu: '1', memory: '2Gi', disk: '5Gi' } }),
    );
  });

  it('blocks saving an environment whose image is not valid', () => {
    const { dialog } = renderWizard(ROW);
    goToStep(dialog, 'Sandbox');
    expect(dialog.getAllByText('runs as root').length).toBeGreaterThan(0);
    expect(dialog.getByRole('button', { name: 'Continue' })).toBeDisabled();
    goToStep(dialog, 'Review');
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();

    goToStep(dialog, 'Sandbox');
    fireEvent.change(dialog.getByLabelText('Sandbox image'), { target: { value: 'kora-base' } });
    goToStep(dialog, 'Review');
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeEnabled();
  });

  it("shows the API's size error on the sandbox step", async () => {
    updateEnvironment.mockResolvedValue({
      environment: null,
      error: 'invalid environment: size.cpu 64 is above the maximum 8',
    });
    const { dialog } = renderWizard({ ...ROW, sandboxImage: 'kora-base' });
    goToStep(dialog, 'Sandbox');
    fireEvent.change(dialog.getByLabelText('CPU'), { target: { value: '64' } });
    goToStep(dialog, 'Review');
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    expect(
      await dialog.findByText('invalid environment: size.cpu 64 is above the maximum 8'),
    ).toBeInTheDocument();
    expect(value(dialog, 'CPU')).toBe('64');
  });

  it('keeps the current image selectable when the image list did not load', () => {
    const { dialog } = renderWizard(null, null);
    toSandbox(dialog);
    expect(dialog.getByText('Could not load the registered images.')).toBeInTheDocument();
    const select = dialog.getByLabelText('Sandbox image') as HTMLSelectElement;
    expect([...select.options].map((o) => [o.text, o.disabled])).toEqual([['kora-base', false]]);
  });
});

describe('stepForError', () => {
  it.each([
    ['invalid environment: size.memory "x" is not a quantity', 1],
    ['invalid environment: sandbox image "acme" is not registered', 1],
    ['invalid environment: sandbox image "acme-ops" is not valid: runs as root', 1],
    [
      'invalid environment: sandbox_image "A_B": a lowercase RFC 1123 label must consist of lower case alphanumeric characters',
      1,
    ],
    ['invalid environment: connectivity host "a b" is invalid', 2],
    ['invalid environment: connector "github" must list at least one action', 2],
    ['invalid environment: connector connection name must not be empty', 2],
    ['invalid environment: connectivity host "pypi.size.dev:443" is invalid', 2],
    ['invalid environment: environment name must not be empty', 0],
    ['environment name already taken for this tenant', 0],
    ['name is required', 0],
    ['Network error: could not reach the server.', 3],
  ])('routes %s to step %i', (message, step) => {
    expect(stepForError(message)).toBe(step);
  });
});
