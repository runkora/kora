import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { TooltipProvider } from '@orbit/terra';
import type { EnvironmentSizes, SandboxImage } from '@orbit/shared/api';
import { EnvironmentWizard, nameFromImage, stepForError } from './environment-wizard';
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
  slug: 'ops',
  name: 'Ops',
  description: '',
  sandboxImage: 'acme-ops',
  size: { cpu: '1', memory: '2Gi', disk: '5Gi' },
  egressHosts: [],
  connectors: [],
  createdAt: '',
  updatedAt: '',
};

const REVIEW_ROW: EnvironmentRow = {
  id: 'env-2',
  slug: 'code-review',
  name: 'Code review',
  description: 'Reviews pull requests.',
  sandboxImage: 'acme-go',
  size: { cpu: '8', memory: '16Gi', disk: '50Gi' },
  egressHosts: ['api.github.com'],
  connectors: [{ connection: 'github', actions: ['repo:read'] }],
  createdAt: '',
  updatedAt: '',
};

const SIZES: EnvironmentSizes = {
  default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
};

function renderWizard(
  environment: EnvironmentRow | null,
  {
    images = IMAGES,
    sizes = SIZES,
    environments = [REVIEW_ROW],
  }: {
    images?: SandboxImage[] | null;
    sizes?: EnvironmentSizes | null;
    environments?: EnvironmentRow[];
  } = {},
) {
  const onSaved = vi.fn();
  render(
    <TooltipProvider>
      <EnvironmentWizard
        environment={environment}
        environments={environments}
        connections={[]}
        images={images}
        sizes={sizes}
        onClose={vi.fn()}
        onSaved={onSaved}
      />
    </TooltipProvider>,
  );
  return { dialog: within(screen.getByRole('dialog')), onSaved };
}

type Dialog = ReturnType<typeof within>;

// A required field's label ends with an asterisk.
const field = (dialog: Dialog, label: string) => dialog.getByLabelText(new RegExp(`^${label}`));

const value = (dialog: Dialog, label: string) => (field(dialog, label) as HTMLInputElement).value;

const type = (dialog: Dialog, label: string, text: string) =>
  fireEvent.change(field(dialog, label), { target: { value: text } });

const continueButton = (dialog: Dialog) => dialog.getByRole('button', { name: 'Continue' });

const next = (dialog: Dialog) => fireEvent.click(continueButton(dialog));

// The desktop rail's step buttons carry the step title; the mobile rail's carry only the number.
const goToStep = (dialog: Dialog, title: string) =>
  fireEvent.click(dialog.getByRole('button', { name: new RegExp(title) }));

const choose = (dialog: Dialog, radio: string) =>
  fireEvent.click(dialog.getByRole('radio', { name: radio }));

/** Moves from Start to Review and creates the environment. */
async function createFromStart(dialog: Dialog, onSaved: () => void) {
  next(dialog);
  next(dialog);
  next(dialog);
  fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
  await waitFor(() => expect(onSaved).toHaveBeenCalled());
}

describe('EnvironmentWizard create', () => {
  beforeEach(() => {
    createEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
    updateEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
  });

  it('starts from the base image with a name, slug and description that follow it', () => {
    const { dialog } = renderWizard(null);
    expect(
      dialog.getByText('Start from a sandbox image, or copy an environment you already have.'),
    ).toBeInTheDocument();
    expect([value(dialog, 'Name'), value(dialog, 'Slug'), value(dialog, 'Description')]).toEqual([
      'Base',
      'base',
      'Go and Python.',
    ]);
    choose(dialog, 'acme-go');
    expect([value(dialog, 'Name'), value(dialog, 'Slug'), value(dialog, 'Description')]).toEqual([
      'Go',
      'go',
      'Go and buf.',
    ]);
    expect(continueButton(dialog)).toBeEnabled();
  });

  it('derives the slug from the name until the slug is edited', () => {
    const { dialog } = renderWizard(null);
    type(dialog, 'Name', 'Payments / Review');
    expect(value(dialog, 'Slug')).toBe('payments-review');
    type(dialog, 'Slug', 'Payments Bot');
    expect(value(dialog, 'Slug')).toBe('payments-bot');
    type(dialog, 'Name', 'Something else');
    expect(value(dialog, 'Slug')).toBe('payments-bot');
  });

  it('needs a name and a valid slug before leaving the start step', () => {
    const { dialog } = renderWizard(null);
    type(dialog, 'Name', '  ');
    expect(dialog.getByText('Enter a name.')).toBeInTheDocument();
    expect(dialog.getByText('Enter a name before you continue.')).toBeInTheDocument();
    expect(continueButton(dialog)).toBeDisabled();

    type(dialog, 'Name', 'Go');
    type(dialog, 'Slug', '-');
    expect(dialog.getByText('Use lowercase letters, digits and hyphens.')).toBeInTheDocument();
    expect(dialog.getByText('Fix the slug before you continue.')).toBeInTheDocument();
    expect(continueButton(dialog)).toBeDisabled();
  });

  it('does not let an invalid image be chosen', () => {
    const { dialog } = renderWizard(null);
    expect(dialog.getByRole('radio', { name: 'acme-ops' })).toBeDisabled();
    expect(dialog.getByText('Not valid: runs as root.')).toBeInTheDocument();
  });

  it('sends the slug, name, image and the default size', async () => {
    const { dialog, onSaved } = renderWizard(null);
    choose(dialog, 'acme-go');
    type(dialog, 'Name', 'Go services');
    await createFromStart(dialog, onSaved);
    expect(createEnvironment).toHaveBeenCalledWith({
      slug: 'go-services',
      name: 'Go services',
      description: 'Go and buf.',
      sandbox_image: 'acme-go',
      size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
      egress_hosts: [],
      connectors: [],
    });
  });

  it('copies an environment with everything it has', async () => {
    const { dialog, onSaved } = renderWizard(null);
    choose(dialog, 'Copy an environment');
    choose(dialog, 'Code review');
    expect([value(dialog, 'Name'), value(dialog, 'Slug'), value(dialog, 'Description')]).toEqual([
      'Code review copy',
      'code-review-copy',
      'Reviews pull requests.',
    ]);
    next(dialog);
    expect(dialog.getByRole('radio', { name: 'Large' })).toBeChecked();
    next(dialog);
    next(dialog);
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        slug: 'code-review-copy',
        sandbox_image: 'acme-go',
        size: { cpu: '8', memory: '16Gi', disk: '50Gi' },
        egress_hosts: ['api.github.com'],
        connectors: [{ connection: 'github', actions: ['repo:read'] }],
      }),
    );
  });

  it('drops what a copy filled in when going back to an image', async () => {
    const { dialog, onSaved } = renderWizard(null);
    choose(dialog, 'Copy an environment');
    choose(dialog, 'Code review');
    choose(dialog, 'Start from an image');
    expect([value(dialog, 'Name'), value(dialog, 'Slug')]).toEqual(['Base', 'base']);
    await createFromStart(dialog, onSaved);
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        sandbox_image: 'kora-base',
        size: SIZES.default,
        egress_hosts: [],
        connectors: [],
      }),
    );
  });

  it('lets a hyphen be typed into the slug', () => {
    const { dialog } = renderWizard(null);
    type(dialog, 'Slug', 'go-');
    expect(value(dialog, 'Slug')).toBe('go-');
    expect(dialog.getByText('Use lowercase letters, digits and hyphens.')).toBeInTheDocument();
    type(dialog, 'Slug', 'go-s');
    expect(value(dialog, 'Slug')).toBe('go-s');
  });

  it('needs an environment chosen before continuing from a copy', () => {
    const { dialog } = renderWizard(null);
    choose(dialog, 'Copy an environment');
    expect(dialog.getByText('Choose an environment to copy.')).toBeInTheDocument();
    expect(continueButton(dialog)).toBeDisabled();
    choose(dialog, 'Code review');
    expect(continueButton(dialog)).toBeEnabled();
  });

  it('blocks creating from the Review step with an invalid Custom size', () => {
    const { dialog } = renderWizard(null);
    next(dialog);
    next(dialog);
    next(dialog);
    goToStep(dialog, 'Sandbox');
    choose(dialog, 'Custom');
    type(dialog, 'CPU', '12');
    goToStep(dialog, 'Review');
    expect(dialog.getByText('Fix the size before you continue.')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Create environment' })).toBeDisabled();
  });

  it('says so when there is no environment to copy', () => {
    const { dialog } = renderWizard(null, { environments: [] });
    choose(dialog, 'Copy an environment');
    expect(dialog.getByText('There are no environments to copy yet.')).toBeInTheDocument();
  });

  it('checks Custom sizes against the maximum and sends them in GiB', async () => {
    const { dialog, onSaved } = renderWizard(null);
    next(dialog);
    expect(dialog.getByRole('radio', { name: 'Medium' })).toBeChecked();
    choose(dialog, 'Custom');
    type(dialog, 'CPU', '12');
    expect(dialog.getByText('CPU can be at most 8 cores.')).toBeInTheDocument();
    expect(dialog.getByText('Fix the size before you continue.')).toBeInTheDocument();
    expect(continueButton(dialog)).toBeDisabled();
    type(dialog, 'CPU', '4');
    type(dialog, 'Memory', '6');
    next(dialog);
    next(dialog);
    expect(dialog.getByText('Custom: 4 cores · 6 GiB · 10 GiB')).toBeInTheDocument();
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({ size: { cpu: '4', memory: '6Gi', disk: '10Gi' } }),
    );
  });

  it('sends the Small preset and the maximum for Large', async () => {
    const { dialog, onSaved } = renderWizard(null);
    next(dialog);
    choose(dialog, 'Small');
    next(dialog);
    next(dialog);
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenLastCalledWith(
      expect.objectContaining({ size: { cpu: '1', memory: '2Gi', disk: '5Gi' } }),
    );

    goToStep(dialog, 'Sandbox');
    choose(dialog, 'Large');
    goToStep(dialog, 'Review');
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(createEnvironment).toHaveBeenCalledTimes(2));
    expect(createEnvironment).toHaveBeenLastCalledWith(
      expect.objectContaining({ size: SIZES.max }),
    );
  });

  it('returns to the start step with the API error when the slug is taken', async () => {
    createEnvironment.mockResolvedValue({
      environment: null,
      error: 'environment slug already taken for this tenant',
    });
    const { dialog } = renderWizard(null);
    next(dialog);
    next(dialog);
    next(dialog);
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    expect(
      await dialog.findByText('environment slug already taken for this tenant'),
    ).toBeInTheDocument();
    expect(field(dialog, 'Slug')).toBeInTheDocument();
  });

  it('shows a note in place of the table when only the base image exists', () => {
    const { dialog } = renderWizard(null, { images: IMAGES.filter((i) => i.name === 'kora-base') });
    next(dialog);
    expect(
      dialog.getByText(/Only the base image is available. Platform teams can register more./),
    ).toBeInTheDocument();
    expect(dialog.getByRole('link', { name: 'Read the sandbox image guide' })).toBeInTheDocument();
    expect(dialog.queryByPlaceholderText('Search images')).not.toBeInTheDocument();
  });

  it('carries on without the image list or the size limits', async () => {
    const { dialog, onSaved } = renderWizard(null, { images: null, sizes: null });
    expect(dialog.getByText('Could not load the registered images.')).toBeInTheDocument();
    next(dialog);
    expect(
      dialog.getByText('Could not load the size limits. They are checked when you save.'),
    ).toBeInTheDocument();
    next(dialog);
    next(dialog);
    fireEvent.click(dialog.getByRole('button', { name: 'Create environment' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(createEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        sandbox_image: 'kora-base',
        size: { cpu: '', memory: '', disk: '' },
      }),
    );
  });
});

describe('EnvironmentWizard edit', () => {
  beforeEach(() => {
    updateEnvironment.mockReset().mockResolvedValue({ environment: null, error: null });
  });

  it('opens on the name with the slug locked', () => {
    const { dialog } = renderWizard(REVIEW_ROW);
    expect(screen.getByRole('dialog', { name: 'Edit environment' })).toBeInTheDocument();
    expect(dialog.getAllByText('Code review').length).toBeGreaterThan(0);
    expect(dialog.queryByText('Start from an image')).not.toBeInTheDocument();
    expect(value(dialog, 'Name')).toBe('Code review');
    expect(value(dialog, 'Slug')).toBe('code-review');
    expect(field(dialog, 'Slug')).toHaveAttribute('readonly');
    expect(
      dialog.getByText('Agents select the environment by its slug. It cannot change.'),
    ).toBeInTheDocument();
  });

  it('saves a new name under the same slug and keeps the saved size', async () => {
    const { dialog, onSaved } = renderWizard(REVIEW_ROW);
    type(dialog, 'Name', 'Payments review');
    expect(value(dialog, 'Slug')).toBe('code-review');
    goToStep(dialog, 'Review');
    expect(
      dialog.getByText('Check the details below, then save your changes.'),
    ).toBeInTheDocument();
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(updateEnvironment).toHaveBeenCalledWith(
      'env-2',
      expect.objectContaining({
        slug: 'code-review',
        name: 'Payments review',
        size: SIZES.max,
      }),
    );
  });

  it('keeps a saved slug that predates the slug rule', async () => {
    const { dialog, onSaved } = renderWizard({ ...REVIEW_ROW, slug: 'Legacy Env' });
    expect(continueButton(dialog)).toBeEnabled();
    goToStep(dialog, 'Review');
    fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(updateEnvironment).toHaveBeenCalledWith(
      'env-2',
      expect.objectContaining({ slug: 'Legacy Env' }),
    );
  });

  it('opens a saved size that matches no preset as Custom', () => {
    const { dialog } = renderWizard({
      ...REVIEW_ROW,
      size: { cpu: '3', memory: '6Gi', disk: '20Gi' },
    });
    goToStep(dialog, 'Sandbox');
    expect(dialog.getByRole('radio', { name: 'Custom' })).toBeChecked();
    expect([value(dialog, 'CPU'), value(dialog, 'Memory'), value(dialog, 'Local disk')]).toEqual([
      '3',
      '6',
      '20',
    ]);
  });

  it('blocks saving an environment whose image is not valid', () => {
    const { dialog } = renderWizard(ROW);
    goToStep(dialog, 'Sandbox');
    expect(dialog.getByText('Not valid: runs as root.')).toBeInTheDocument();
    expect(continueButton(dialog)).toBeDisabled();
    goToStep(dialog, 'Review');
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();

    goToStep(dialog, 'Sandbox');
    choose(dialog, 'kora-base');
    goToStep(dialog, 'Review');
    expect(dialog.getByRole('button', { name: 'Save changes' })).toBeEnabled();
  });

  it('does not block the Identity step on an image it does not show', () => {
    const { dialog } = renderWizard(ROW);
    expect(continueButton(dialog)).toBeEnabled();
  });

  it('ignores typing in the locked slug', () => {
    const { dialog } = renderWizard(REVIEW_ROW);
    type(dialog, 'Slug', 'other');
    expect(value(dialog, 'Slug')).toBe('code-review');
  });

  it('reports an image that is not registered', () => {
    const { dialog } = renderWizard({ ...ROW, sandboxImage: 'acme-gone' });
    goToStep(dialog, 'Sandbox');
    expect(dialog.getByText('acme-gone: Not registered in this tenant.')).toBeInTheDocument();
  });
});

describe('nameFromImage', () => {
  it.each([
    ['acme-go', 'Go'],
    ['kora-base', 'Base'],
    ['payments-node-browsers', 'Node browsers'],
    ['plain', 'Plain'],
  ])('names %s as %s', (image, name) => {
    expect(nameFromImage(image)).toBe(name);
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
    ['environment slug already taken for this tenant', 0],
    ['invalid environment: slug cannot change after create', 0],
    ['name is required', 0],
    ['Network error: could not reach the server.', 3],
  ])('routes %s to step %i', (message, step) => {
    expect(stepForError(message)).toBe(step);
  });
});
