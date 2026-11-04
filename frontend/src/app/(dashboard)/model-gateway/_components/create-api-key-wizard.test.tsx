import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { TooltipProvider } from '@runkora/hades';

const createToken = vi.fn();

vi.mock('../_actions/tokens', () => ({
  createToken: (...args: unknown[]) => createToken(...args),
}));

import { CreateApiKeyWizard } from './create-api-key-wizard';

const SCOPES = ['agents:read', 'agents:write'];

const PAT = {
  id: 'pat_1',
  name: 'CI key',
  token: 'kora_pat_secret',
  scopes: SCOPES,
  expires_at: null,
};

function renderWizard(availableScopes = SCOPES) {
  const onClose = vi.fn();
  const onCreated = vi.fn();
  render(
    <TooltipProvider>
      <CreateApiKeyWizard
        availableScopes={availableScopes}
        onClose={onClose}
        onCreated={onCreated}
      />
    </TooltipProvider>,
  );
  return { onClose, onCreated };
}

const next = () => fireEvent.click(screen.getByRole('button', { name: /continue/i }));
const nameInput = () => screen.getByLabelText(/^Name/);
const expiryInput = () => screen.getByLabelText(/Expires in/);
const pickScope = () => fireEvent.click(screen.getAllByRole('checkbox')[0]!);
const createButton = () => screen.getByRole('button', { name: 'Create key' });

function fillName(value = 'CI key') {
  fireEvent.change(nameInput(), { target: { value } });
}

describe('CreateApiKeyWizard', () => {
  beforeEach(() => createToken.mockReset());

  it('holds Continue until the key has a name', () => {
    renderWizard();
    expect(screen.getByText('Name the key before you continue.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fillName('   ');
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
    fillName();
    expect(screen.getByRole('button', { name: /continue/i })).toBeEnabled();
  });

  it.each(['0', '366', '1.5'])('rejects an expiry of %s days', (value) => {
    renderWizard();
    fillName();
    fireEvent.change(expiryInput(), { target: { value } });
    expect(screen.getByText('Expiry must be between 1 and 365 days.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });

  it('requires a permission, then creates the key and shows it once', async () => {
    createToken.mockResolvedValue({ ok: true, pat: PAT });
    const { onCreated } = renderWizard();
    fillName();
    next();
    expect(
      screen.getByText('Choose at least one permission before you continue.'),
    ).toBeInTheDocument();
    pickScope();
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1));
    expect(createToken).toHaveBeenCalledWith({
      name: 'CI key',
      expires_in_days: 90,
      scopes: SCOPES,
    });
    expect(await screen.findByText('API key created')).toBeInTheDocument();
    expect(screen.getByText('Copy this key now.')).toBeInTheDocument();
    expect(screen.getByText('Never expires')).toBeInTheDocument();
    expect(onCreated).toHaveBeenCalledTimes(1);
  });

  it('sends no expiry when the field is cleared', async () => {
    createToken.mockResolvedValue({ ok: true, pat: PAT });
    renderWizard();
    fillName();
    fireEvent.change(expiryInput(), { target: { value: '' } });
    next();
    pickScope();
    next();
    fireEvent.click(createButton());
    await waitFor(() => expect(createToken).toHaveBeenCalled());
    expect(createToken.mock.calls[0]![0].expires_in_days).toBeUndefined();
  });

  it('keeps the wizard on review with the server message when creation fails', async () => {
    createToken.mockResolvedValue({ ok: false, status: 403, message: 'Scope exceeds access' });
    const { onCreated } = renderWizard();
    fillName();
    next();
    pickScope();
    next();
    fireEvent.click(createButton());
    expect(await screen.findByText('Scope exceeds access')).toBeInTheDocument();
    expect(createButton()).toBeEnabled();
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('shows a retryable error when the request throws', async () => {
    createToken.mockRejectedValueOnce(new Error('network'));
    renderWizard();
    fillName();
    next();
    pickScope();
    next();
    fireEvent.click(createButton());
    expect(
      await screen.findByText('Could not create the key. Check your connection and try again.'),
    ).toBeInTheDocument();
  });

  it('resets for another key and closes from Done', async () => {
    createToken.mockResolvedValue({ ok: true, pat: PAT });
    const { onClose } = renderWizard();
    fillName();
    next();
    pickScope();
    next();
    fireEvent.click(createButton());
    await screen.findByText('API key created');

    fireEvent.click(screen.getByRole('button', { name: /create another/i }));
    expect(nameInput()).toHaveValue('');
    expect(expiryInput()).toHaveValue(90);

    fillName('Second');
    next();
    pickScope();
    next();
    createToken.mockResolvedValue({ ok: true, pat: { ...PAT, token: 'second' } });
    fireEvent.click(createButton());
    await screen.findByText('API key created');
    fireEvent.click(screen.getByRole('button', { name: /done, i've copied it/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('explains when the account has no scopes to give', () => {
    renderWizard([]);
    fillName();
    next();
    expect(screen.getByText(/No scopes available/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled();
  });
});
