import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { TooltipProvider } from '@runkora/hades';

const createGrantAction = vi.fn();

vi.mock('../_actions/grants', () => ({
  createGrantAction: (...args: unknown[]) => createGrantAction(...args),
  searchUsersAction: () =>
    Promise.resolve({
      ok: true,
      data: [{ id: 'usr_1', email: 'maya@example.com', display_name: 'Maya Chen' }],
    }),
}));

import { CreateGrantWizard } from './create-grant-wizard';

const SCOPES = ['agents:read', 'agents:write'];

function renderWizard() {
  const onClose = vi.fn();
  const onCreated = vi.fn();
  const ui = (
    <TooltipProvider>
      <CreateGrantWizard availableScopes={SCOPES} onClose={onClose} onCreated={onCreated} />
    </TooltipProvider>
  );
  const view = render(ui);
  return { onClose, onCreated, ...view };
}

const next = () => fireEvent.click(screen.getByRole('button', { name: /continue/i }));

async function pickUser() {
  fireEvent.click(await screen.findByRole('radio', { name: /Maya Chen/ }));
}

const pickScope = () => fireEvent.click(screen.getAllByRole('checkbox')[0]!);
const createButton = () => screen.getByRole('button', { name: 'Create grant' });

/** Fills user and a scope, then lands on the budget step. */
async function toBudget() {
  await pickUser();
  next();
  pickScope();
  next();
}

describe('CreateGrantWizard', () => {
  beforeEach(() => createGrantAction.mockReset());

  it('offers no principal type and holds Continue until a user is chosen', async () => {
    renderWizard();
    expect(screen.queryByRole('radio', { name: /agent|trigger|service|pat/i })).toBeNull();
    expect(screen.queryByRole('combobox')).toBeNull();
    expect(screen.getByText('Choose a user before you continue.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    await pickUser();
    expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled();
  });

  it('requires a scope, then creates a HUMAN grant with no budget', async () => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    const { onCreated } = renderWizard();
    await pickUser();
    next();
    expect(screen.getByText('Choose at least one scope before you continue.')).toBeInTheDocument();
    pickScope();
    next(); // budget, left off
    next(); // review
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalledTimes(1));
    expect(createGrantAction).toHaveBeenCalledWith({
      principal_type: 'HUMAN',
      principal_id: 'usr_1',
      scopes: SCOPES,
      budget: null,
    });
    expect(await screen.findByText('Grant created')).toBeInTheDocument();
    expect(onCreated).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['monthly', 'Monthly'],
    ['weekly', 'Weekly'],
  ])('sends a %s budget when a limit is set', async (cadence, label) => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    expect(
      screen.getByText('Enter a spend limit or a per-run limit, or turn the budget off.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '25' } });
    fireEvent.click(screen.getByRole('button', { name: label }));
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalled());
    expect(createGrantAction.mock.calls[0]![0].budget).toEqual({
      limit_usd: 25,
      reset_cadence: cadence,
    });
  });

  it.each([
    ['window only', '25', '', { limit_usd: 25 }],
    ['per-run only', '', '4', { per_run_limit_usd: 4 }],
    ['both', '25', '4', { limit_usd: 25, per_run_limit_usd: 4 }],
  ])('sends a %s budget', async (_name, window, perRunValue, expected) => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    if (window)
      fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: window } });
    if (perRunValue)
      fireEvent.change(screen.getByPlaceholderText('e.g. 5'), { target: { value: perRunValue } });
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalled());
    expect(createGrantAction.mock.calls[0]![0].budget).toEqual({
      reset_cadence: 'monthly',
      ...expected,
    });
  });

  it.each([
    ['both empty', '', ''],
    ['both zero', '0', '0'],
    ['a negative per-run limit', '', '-1'],
  ])('holds Continue on the budget step with %s', async (_name, window, perRunValue) => {
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    if (window)
      fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: window } });
    if (perRunValue)
      fireEvent.change(screen.getByPlaceholderText('e.g. 5'), { target: { value: perRunValue } });
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('shows both limits on the review step', async () => {
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '25' } });
    fireEvent.change(screen.getByPlaceholderText('e.g. 5'), { target: { value: '4' } });
    next();
    expect(screen.getByText('Per-run limit')).toBeInTheDocument();
    expect(screen.getByText(/\$4\.00 per run/)).toBeInTheDocument();
    expect(screen.getByText(/\$25\.00/)).toBeInTheDocument();
  });

  it('sends no budget when it is switched on and then off again', async () => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '25' } });
    fireEvent.click(screen.getByRole('switch'));
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalled());
    expect(createGrantAction.mock.calls[0]![0].budget).toBeNull();
  });

  it('keeps the wizard open on review and shows the error when the create fails', async () => {
    createGrantAction.mockResolvedValue({ ok: false, status: 403, message: 'forbidden' });
    const { onCreated } = renderWizard();
    await toBudget();
    next();
    fireEvent.click(createButton());
    expect(await screen.findByText('forbidden')).toBeInTheDocument();
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('shows an error and stays usable when the action throws', async () => {
    createGrantAction.mockRejectedValueOnce(new Error('network down'));
    const { onClose, onCreated } = renderWizard();
    await toBudget();
    next();
    fireEvent.click(createButton());
    expect(await screen.findByText(/Could not create the grant/)).toBeInTheDocument();
    expect(onCreated).not.toHaveBeenCalled();
    // submitting is cleared: Create is enabled again and the wizard closes.
    expect(createButton()).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Close Create grant' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('disables Create again after jumping back and emptying the scopes', async () => {
    renderWizard();
    await toBudget();
    next();
    expect(createButton()).toBeEnabled();
    fireEvent.click(screen.getAllByRole('button', { name: 'Edit' })[1]!); // the scopes card
    pickScope(); // unchecks the only selection
    expect(screen.getByText('Choose at least one scope before you continue.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('resets every field on Create another', async () => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(screen.getByRole('switch'));
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '25' } });
    fireEvent.click(screen.getByRole('button', { name: 'Weekly' }));
    next();
    fireEvent.click(createButton());
    fireEvent.click(await screen.findByRole('button', { name: /create another/i }));

    expect(screen.getByText('Choose a user before you continue.')).toBeInTheDocument();
    await pickUser();
    next();
    expect(screen.getByText('Choose at least one scope before you continue.')).toBeInTheDocument();
    pickScope();
    next();
    expect(screen.getByRole('switch')).not.toBeChecked();
    expect(screen.queryByPlaceholderText('e.g. 50')).toBeNull();
    fireEvent.click(screen.getByRole('switch'));
    expect(screen.getByPlaceholderText('e.g. 50')).toHaveValue(null);
    // The cadence is back to monthly.
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '10' } });
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalledTimes(2));
    expect(createGrantAction.mock.calls[1]![0].budget).toEqual({
      limit_usd: 10,
      reset_cadence: 'monthly',
    });
  });

  it('starts clean when it is closed and opened again', async () => {
    const first = renderWizard();
    await toBudget();
    first.unmount();
    renderWizard();
    expect(screen.getByText('Choose a user before you continue.')).toBeInTheDocument();
  });

  describe('hint strip', () => {
    const strip = () => {
      const all = document.querySelectorAll('[data-slot="grant-wizard-hint"]');
      expect(all).toHaveLength(1);
      return all[0] as HTMLElement;
    };
    /** The strip's place in the layout: its parent and what comes before it. */
    const shape = (el: HTMLElement) => ({
      parent: el.parentElement,
      index: Array.from(el.parentElement!.children).indexOf(el),
      tag: el.tagName,
      children: Array.from(el.children).map((c) => c.tagName),
    });

    it('is always rendered, in the same place and shape, with or without a hint', async () => {
      renderWizard();
      const withHint = strip();
      expect(withHint).toHaveAttribute('role', 'alert');
      const before = shape(withHint);

      await pickUser(); // step 1 becomes valid: the hint clears
      const cleared = strip();
      expect(cleared).toHaveAttribute('role', 'status');
      expect(cleared).toHaveTextContent('Ready to continue.');
      expect(shape(cleared)).toEqual(before);
      expect(cleared.parentElement!.firstElementChild).toBe(cleared);
    });

    it('keeps the same place on every step, including review', async () => {
      renderWizard();
      const first = shape(strip());
      await pickUser();
      next();
      expect(shape(strip()).index).toBe(first.index); // scopes, with a hint
      pickScope();
      next();
      expect(shape(strip()).index).toBe(first.index); // budget, valid
      fireEvent.click(screen.getByRole('switch'));
      expect(strip()).toHaveAttribute('role', 'alert'); // budget, invalid
      expect(shape(strip()).index).toBe(first.index);
      fireEvent.click(screen.getByRole('switch'));
      next();
      expect(shape(strip()).index).toBe(first.index); // review
    });

    it('does not use the Wizard hint strip, which mounts only while a hint is set', () => {
      renderWizard();
      expect(document.querySelector('[data-slot="wizard"] > [role="alert"]')).toBeNull();
    });

    it('is absent from the success screen', async () => {
      createGrantAction.mockResolvedValue({ ok: true, data: {} });
      renderWizard();
      await toBudget();
      next();
      fireEvent.click(createButton());
      await screen.findByText('Grant created');
      expect(document.querySelector('[data-slot="grant-wizard-hint"]')).toBeNull();
    });
  });
});
