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
const spendSwitch = () => screen.getByRole('switch', { name: 'Spend limit' });
const perRunSwitch = () => screen.getByRole('switch', { name: 'Per-run limit' });
const spendInput = () => screen.getByLabelText('Spend limit in USD');
const perRunInput = () => screen.getByLabelText('Per-run limit in USD');
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
  ])('sends a %s spend limit', async (cadence, label) => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(spendSwitch());
    expect(screen.getByText('Enter a spend limit above 0.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fireEvent.change(spendInput(), { target: { value: '25' } });
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
    ['spend only', '25', '', { limit_usd: 25 }],
    ['per-run only', '', '4', { per_run_limit_usd: 4 }],
    ['both', '25', '4', { limit_usd: 25, per_run_limit_usd: 4 }],
  ])('sends a %s budget', async (_name, window, perRunValue, expected) => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    if (window) {
      fireEvent.click(spendSwitch());
      fireEvent.change(spendInput(), { target: { value: window } });
    }
    if (perRunValue) {
      fireEvent.click(perRunSwitch());
      fireEvent.change(perRunInput(), { target: { value: perRunValue } });
    }
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createGrantAction).toHaveBeenCalled());
    expect(createGrantAction.mock.calls[0]![0].budget).toEqual({
      reset_cadence: 'monthly',
      ...expected,
    });
  });

  it('keeps both rows and their controls in place whether a row is on or off', async () => {
    renderWizard();
    await toBudget();
    expect(spendSwitch()).not.toBeChecked();
    expect(perRunSwitch()).not.toBeChecked();
    expect(spendInput()).toBeInTheDocument();
    expect(perRunInput()).toBeInTheDocument();
    fireEvent.click(spendSwitch());
    expect(spendInput()).toBeInTheDocument();
    expect(perRunInput()).toBeInTheDocument();
  });

  it.each([
    ['spend on but empty', '', '', true, false],
    ['spend on and zero', '0', '', true, false],
    ['spend on and negative', '-5', '', true, false],
    ['per-run on and negative', '', '-1', false, true],
    ['per-run on and zero', '', '0', false, true],
  ])('holds Continue with %s', async (_name, window, perRunValue, spendOn, perRunOn) => {
    renderWizard();
    await toBudget();
    if (spendOn) fireEvent.click(spendSwitch());
    if (perRunOn) fireEvent.click(perRunSwitch());
    if (window) fireEvent.change(spendInput(), { target: { value: window } });
    if (perRunValue) fireEvent.change(perRunInput(), { target: { value: perRunValue } });
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('continues with no budget when both rows are off', async () => {
    renderWizard();
    await toBudget();
    expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled();
  });

  it('shows only the rows that are on in the review', async () => {
    renderWizard();
    await toBudget();
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '25' } });
    fireEvent.click(screen.getByRole('button', { name: 'Weekly' }));
    next();
    expect(screen.getByText('Spend limit')).toBeInTheDocument();
    expect(screen.getByText('$25.00 per week')).toBeInTheDocument();
    expect(screen.queryByText('Per-run limit')).toBeNull();
    expect(screen.queryByText('No budget')).toBeNull();
  });

  it('shows both limits on the review step', async () => {
    renderWizard();
    await toBudget();
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '25' } });
    fireEvent.click(perRunSwitch());
    fireEvent.change(perRunInput(), { target: { value: '4' } });
    next();
    expect(screen.getByText('$25.00 per month')).toBeInTheDocument();
    expect(screen.getByText('$4.00 per run')).toBeInTheDocument();
  });

  it('says "No budget" in the review when both rows are off', async () => {
    renderWizard();
    await toBudget();
    next();
    expect(screen.getByText('No budget')).toBeInTheDocument();
    expect(screen.queryByText('Spend limit')).toBeNull();
  });

  it('sends no budget when a row is switched on and then off again', async () => {
    createGrantAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    await toBudget();
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '25' } });
    fireEvent.click(spendSwitch());
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
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '25' } });
    fireEvent.click(screen.getByRole('button', { name: 'Weekly' }));
    fireEvent.click(perRunSwitch());
    fireEvent.change(perRunInput(), { target: { value: '4' } });
    next();
    fireEvent.click(createButton());
    fireEvent.click(await screen.findByRole('button', { name: /create another/i }));

    expect(screen.getByText('Choose a user before you continue.')).toBeInTheDocument();
    await pickUser();
    next();
    expect(screen.getByText('Choose at least one scope before you continue.')).toBeInTheDocument();
    pickScope();
    next();
    expect(spendSwitch()).not.toBeChecked();
    expect(perRunSwitch()).not.toBeChecked();
    expect(spendInput()).toHaveValue('');
    expect(perRunInput()).toHaveValue('');
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '10' } });
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

  describe('hint banner', () => {
    // The Hades Wizard shows its hint as an alert that is mounted only while there is one.
    const banner = () => screen.queryByRole('alert');

    it('shows the user hint until a user is chosen', async () => {
      renderWizard();
      expect(banner()).toHaveTextContent('Choose a user before you continue.');
      await pickUser();
      expect(banner()).toBeNull();
    });

    it('shows the scope hint until a scope is chosen', async () => {
      renderWizard();
      await pickUser();
      next();
      expect(banner()).toHaveTextContent('Choose at least one scope before you continue.');
      pickScope();
      expect(banner()).toBeNull();
    });

    it('shows the budget hint only while a row is on and invalid', async () => {
      renderWizard();
      await toBudget();
      expect(banner()).toBeNull(); // both rows off: no budget, nothing to fix
      fireEvent.click(spendSwitch());
      expect(banner()).toHaveTextContent('Enter a spend limit above 0.');
      fireEvent.change(spendInput(), { target: { value: '25' } });
      expect(banner()).toBeNull();
      fireEvent.click(perRunSwitch());
      expect(banner()).toHaveTextContent('Enter a per-run limit above 0.');
      fireEvent.click(perRunSwitch());
      expect(banner()).toBeNull();
    });

    it('shows no banner on review or on the success screen', async () => {
      createGrantAction.mockResolvedValue({ ok: true, data: {} });
      renderWizard();
      await toBudget();
      next();
      expect(banner()).toBeNull();
      fireEvent.click(createButton());
      await screen.findByText('Grant created');
      expect(banner()).toBeNull();
    });
  });
});
