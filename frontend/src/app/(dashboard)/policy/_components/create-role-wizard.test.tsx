import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { TooltipProvider } from '@runkora/hades';

const upsertRoleAction = vi.fn();

vi.mock('../_actions/roles', () => ({
  upsertRoleAction: (...args: unknown[]) => upsertRoleAction(...args),
}));

import { CreateRoleWizard } from './create-role-wizard';

const SCOPES = ['agents:read', 'agents:write'];

function renderWizard(props: { scopesError?: string; existing?: string[] } = {}) {
  const onClose = vi.fn();
  const onCreated = vi.fn();
  render(
    <TooltipProvider>
      <CreateRoleWizard
        availableScopes={SCOPES}
        scopesError={props.scopesError ?? null}
        existingRoleNames={props.existing ?? ['engineer']}
        onClose={onClose}
        onCreated={onCreated}
      />
    </TooltipProvider>,
  );
  return { onClose, onCreated };
}

const next = () => fireEvent.click(screen.getByRole('button', { name: /continue/i }));
const fillName = (value = 'ml-engineer') =>
  fireEvent.change(screen.getByLabelText(/Role name/), { target: { value } });
const pickScope = () => fireEvent.click(screen.getAllByRole('checkbox')[0]!);
const spendSwitch = () => screen.getByRole('switch', { name: 'Spend limit' });
const createButton = () => screen.getByRole('button', { name: 'Create role' });

/** Fills a name and a scope, then lands on the budget step. */
function toBudget() {
  fillName();
  next();
  pickScope();
  next();
}

describe('CreateRoleWizard', () => {
  beforeEach(() => upsertRoleAction.mockReset());

  it('holds Continue until the role has a name', () => {
    renderWizard();
    expect(screen.getByText('Name the role before you continue.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fillName('   ');
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fillName();
    expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled();
  });

  it('refuses a name that would replace an existing role', () => {
    renderWizard();
    fillName(' Engineer ');
    expect(screen.getByText(/A role named "Engineer" already exists/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('requires a scope, then creates the role with no budget', async () => {
    upsertRoleAction.mockResolvedValue({ ok: true, data: {} });
    const { onCreated } = renderWizard();
    fillName();
    next();
    expect(screen.getByText('Choose at least one scope before you continue.')).toBeInTheDocument();
    pickScope();
    next(); // budget, left off
    next(); // review
    fireEvent.click(createButton());
    await waitFor(() => expect(upsertRoleAction).toHaveBeenCalledTimes(1));
    expect(upsertRoleAction).toHaveBeenCalledWith({
      role: 'ml-engineer',
      scopes: SCOPES,
      budget: null,
    });
    expect(await screen.findByText('Role created')).toBeInTheDocument();
    expect(onCreated).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['per member', false],
    ['pooled', true],
  ])('sends a %s spend limit', async (_label, pooled) => {
    upsertRoleAction.mockResolvedValue({ ok: true, data: {} });
    renderWizard();
    toBudget();
    fireEvent.click(spendSwitch());
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Spend limit in USD'), { target: { value: '25' } });
    if (pooled) fireEvent.click(screen.getByRole('switch', { name: /pool/i }));
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(upsertRoleAction).toHaveBeenCalled());
    const budget = upsertRoleAction.mock.calls[0]![0].budget;
    expect(budget.limit_usd).toBe(25);
    expect(Boolean(budget.pooled)).toBe(pooled);
  });

  it('cannot choose scopes when the vocabulary failed to load', () => {
    renderWizard({ scopesError: 'No access to the vocabulary.' });
    fillName();
    next();
    expect(screen.getByText('No access to the vocabulary.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('keeps the wizard on review with the server message when creation fails', async () => {
    upsertRoleAction.mockResolvedValue({ ok: false, status: 403, message: 'Forbidden role' });
    const { onCreated } = renderWizard();
    toBudget();
    next();
    fireEvent.click(createButton());
    expect(await screen.findByText('Forbidden role')).toBeInTheDocument();
    expect(createButton()).toBeEnabled();
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('shows a retryable error when the action throws', async () => {
    upsertRoleAction.mockRejectedValueOnce(new Error('network down'));
    renderWizard();
    toBudget();
    next();
    fireEvent.click(createButton());
    expect(await screen.findByText(/Could not create the role/)).toBeInTheDocument();
    expect(createButton()).toBeEnabled();
  });

  it('resets for another role and closes from Done', async () => {
    upsertRoleAction.mockResolvedValue({ ok: true, data: {} });
    const { onClose } = renderWizard();
    toBudget();
    next();
    fireEvent.click(createButton());
    await screen.findByText('Role created');
    fireEvent.click(screen.getByRole('button', { name: /create another/i }));
    expect(screen.getByLabelText(/Role name/)).toHaveValue('');
    fireEvent.click(screen.getByRole('button', { name: 'Close Create role' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
