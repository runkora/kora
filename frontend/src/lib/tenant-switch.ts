/**
 * Browser side of a tenant switch (docs/adr/2026-11-04-tenant-switching.md).
 *
 * A switch is a full-page `prompt=none` sign-in on the shared issuer and
 * client. The PKCE state carries the target tenant and the return path, and the
 * callback page finishes the exchange.
 */
import {
  buildUserManager,
  TENANT_CONFIG_STORAGE_KEY,
  type DiscoverResponse,
  type TenantOidcConfig,
} from '@runkora/lib/oidc-client';
import { ApiRoute } from '@runkora/lib/routing/api-routes';

/** The state a sign-in round-trips through the identity provider. */
export interface SignInState {
  next: string;
  /** The tenant whose issuer and client verify the ID token. */
  tenantId: string;
  /** Set on a switch: the tenant to move to. */
  switchTo?: string;
  /** Set on a switch: the person's email, sent as `login_hint`. */
  email?: string;
}

/** Stores the tenant config for the callback page and redirects to the identity provider. */
export async function redirectToSignIn(
  cfg: TenantOidcConfig,
  state: SignInState,
  prompt?: 'none',
): Promise<void> {
  sessionStorage.setItem(TENANT_CONFIG_STORAGE_KEY, JSON.stringify(cfg));
  await buildUserManager(cfg).signinRedirect({ state, prompt, login_hint: state.email });
}

/**
 * Starts a silent sign-in that moves the session to `tenantId`. Discovery by
 * email returns the issuer and client every tenant in the eligible set shares.
 */
export async function startTenantSwitch({
  email,
  tenantId,
  returnPath,
}: {
  email: string;
  tenantId: string;
  returnPath: string;
}): Promise<void> {
  const res = await fetch(ApiRoute.discover.GET(email), {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) throw new Error(`tenant discovery failed: ${res.status}`);
  const d = (await res.json()) as DiscoverResponse;
  await redirectToSignIn(
    { tenantId: d.tenant_id, issuerUrl: d.issuer_url, clientId: d.client_id, scopes: d.scopes },
    { next: returnPath, tenantId: d.tenant_id, switchTo: tenantId, email },
    'none',
  );
}
