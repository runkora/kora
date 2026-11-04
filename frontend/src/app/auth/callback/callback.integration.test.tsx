/**
 * Integration tests for the OIDC callback page: the tenant picker, the refused
 * switch and the interactive fallback after a failed `prompt=none` sign-in.
 *
 * The OIDC client and the redirect helper are mocked. MSW answers the BFF
 * session POST and records the page's request bodies.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { ErrorResponse } from 'oidc-client-ts';
import { server } from '@runkora/test-utils';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import { buildUserManager, TENANT_CONFIG_STORAGE_KEY } from '@runkora/lib/oidc-client';
import { announceTenantSwitch, redirectToSignIn } from '@runkora/lib/tenant-switch';

import CallbackPage from './page';

const replace = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ replace }) }));
vi.mock('@runkora/lib/oidc-client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@runkora/lib/oidc-client')>()),
  buildUserManager: vi.fn(),
}));
vi.mock('@runkora/lib/tenant-switch', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@runkora/lib/tenant-switch')>()),
  redirectToSignIn: vi.fn(),
  announceTenantSwitch: vi.fn(),
}));

const CONFIG = {
  tenantId: 'example',
  issuerUrl: 'https://idp.example.com',
  clientId: 'client',
  scopes: ['openid'],
};
const SWITCH_STATE = {
  next: '/agents',
  tenantId: 'example',
  switchTo: 'example-data',
  email: 'dev@runkora.test',
};

/** Makes signinRedirectCallback resolve with `state`, or reject with `error`. */
function callbackReturns(result: { state: object } | { error: unknown }) {
  const signinRedirectCallback =
    'error' in result
      ? vi.fn().mockRejectedValue(result.error)
      : vi.fn().mockResolvedValue({ id_token: 'idt', state: result.state });
  vi.mocked(buildUserManager).mockReturnValue({ signinRedirectCallback } as never);
}

/** Answers the session POST with `replies` in turn and records the bodies sent. */
function sessionReplies(...replies: Response[]) {
  const sent: Record<string, unknown>[] = [];
  server.use(
    http.post(ApiRoute.session.POST(), async ({ request }) => {
      sent.push((await request.json()) as Record<string, unknown>);
      return replies.shift() ?? HttpResponse.json({});
    }),
  );
  return sent;
}

beforeEach(() => {
  replace.mockReset();
  vi.mocked(redirectToSignIn).mockReset();
  vi.mocked(announceTenantSwitch).mockReset();
  sessionStorage.setItem(TENANT_CONFIG_STORAGE_KEY, JSON.stringify(CONFIG));
});

describe('CallbackPage', () => {
  it('shows the picker on 409 and re-posts the same ID token with the pick', async () => {
    callbackReturns({ state: { next: '/agents', tenantId: 'example' } });
    const sent = sessionReplies(
      HttpResponse.json(
        {
          error: 'multiple_tenants_match',
          tenants: [
            { id: 'example', displayName: 'Example Platform' },
            { id: 'example-data', displayName: 'Example Data' },
          ],
        },
        { status: 409 },
      ),
      HttpResponse.json({}),
    );
    const user = userEvent.setup();
    render(<CallbackPage />);

    await user.click(await screen.findByRole('button', { name: 'Example Data' }));

    await vi.waitFor(() => expect(replace).toHaveBeenCalledWith('/agents'));
    expect(sent).toEqual([
      { id_token: 'idt', tenant_id: 'example' },
      { id_token: 'idt', tenant_id: 'example', tenant_hint: 'example-data' },
    ]);
  });

  it('shows the refused switch and keeps the person signed in', async () => {
    callbackReturns({ state: SWITCH_STATE });
    const sent = sessionReplies(
      HttpResponse.json({ error: 'tenant_switch_denied' }, { status: 403 }),
    );
    render(<CallbackPage />);

    expect(await screen.findByText('Switch failed')).toBeInTheDocument();
    expect(sent[0]).toEqual({
      id_token: 'idt',
      tenant_id: 'example',
      tenant_hint: 'example-data',
      switch: true,
    });
    expect(replace).not.toHaveBeenCalled();
    expect(announceTenantSwitch).not.toHaveBeenCalled();
  });

  it('tells the other tabs when a switch succeeds', async () => {
    callbackReturns({ state: SWITCH_STATE });
    sessionReplies(HttpResponse.json({}));
    render(<CallbackPage />);

    await vi.waitFor(() => expect(announceTenantSwitch).toHaveBeenCalledOnce());
  });

  it.each([
    'login_required',
    'interaction_required',
    'consent_required',
    'account_selection_required',
  ])('retries a switch interactively on %s', async (error) => {
    callbackReturns({ error: new ErrorResponse({ error, userState: SWITCH_STATE }) });
    render(<CallbackPage />);

    await vi.waitFor(() => expect(redirectToSignIn).toHaveBeenCalledOnce());
    // Two arguments: the retry carries the same state and no prompt=none.
    expect(vi.mocked(redirectToSignIn).mock.calls[0]).toEqual([CONFIG, SWITCH_STATE]);
    expect(replace).not.toHaveBeenCalled();
  });

  it('sends a failed sign-in that is not a switch to the login page', async () => {
    callbackReturns({
      error: new ErrorResponse({
        error: 'login_required',
        userState: { next: '/agents', tenantId: 'example' },
      }),
    });
    render(<CallbackPage />);

    await vi.waitFor(() => expect(replace).toHaveBeenCalledWith('/login?error=server_error'));
    expect(redirectToSignIn).not.toHaveBeenCalled();
  });
});
