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
import { isSafeRedirectPath } from '@runkora/lib/routing';
import { ApiRoute } from '@runkora/lib/routing/api-routes';

/** The state a sign-in round-trips through the identity provider. */
interface SignInState {
  next: string;
  /** The tenant whose issuer and client verify the ID token. */
  tenantId: string;
  /** Set on a switch: the tenant to move to. */
  switchTo?: string;
  /** Set on a switch: the person's email, sent as `login_hint`. */
  email?: string;
}

/**
 * Reads the state the identity provider returned. Null without a tenant; an unsafe or missing
 * return path becomes `/`.
 */
export function readSignInState(value: unknown): SignInState | null {
  const s = (value && typeof value === 'object' ? value : {}) as Record<string, unknown>;
  const text = (v: unknown) => (typeof v === 'string' && v ? v : undefined);
  const tenantId = text(s.tenantId);
  if (!tenantId) return null;
  const next = text(s.next);
  return {
    next: next && isSafeRedirectPath(next) ? next : '/',
    tenantId,
    switchTo: text(s.switchTo),
    email: text(s.email),
  };
}

/** The channel that tells this origin's other tabs a switch finished. */
const SWITCH_CHANNEL = 'kora-tenant-switch';

/** Tells the other open tabs that the session moved to another tenant. */
export function announceTenantSwitch(): void {
  if (typeof BroadcastChannel === 'undefined') return;
  const channel = new BroadcastChannel(SWITCH_CHANNEL);
  channel.postMessage('switched');
  channel.close();
}

/**
 * Calls `onSwitch` when another tab finishes a switch, or when Back restores this page from the
 * back-forward cache, where it may predate a switch. Returns the unsubscribe.
 */
export function onTenantSwitch(onSwitch: () => void): () => void {
  const restored = (event: PageTransitionEvent) => {
    if (event.persisted) onSwitch();
  };
  window.addEventListener('pageshow', restored);
  const channel =
    typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(SWITCH_CHANNEL);
  if (channel) channel.onmessage = onSwitch;
  return () => {
    window.removeEventListener('pageshow', restored);
    channel?.close();
  };
}

/** Checks the fields the sign-in redirect needs and throws on a malformed answer. */
function parseDiscover(body: unknown): DiscoverResponse {
  const d = (body ?? {}) as Partial<Record<keyof DiscoverResponse, unknown>>;
  const nonEmpty = (v: unknown): v is string => typeof v === 'string' && v !== '';
  if (
    !nonEmpty(d.tenant_id) ||
    !nonEmpty(d.issuer_url) ||
    !nonEmpty(d.client_id) ||
    !Array.isArray(d.scopes) ||
    !d.scopes.every((x) => typeof x === 'string')
  ) {
    throw new Error('tenant discovery returned an incomplete config');
  }
  return {
    tenant_id: d.tenant_id,
    issuer_url: d.issuer_url,
    client_id: d.client_id,
    scopes: d.scopes,
  };
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
  const d = parseDiscover(await res.json());
  await redirectToSignIn(
    { tenantId: d.tenant_id, issuerUrl: d.issuer_url, clientId: d.client_id, scopes: d.scopes },
    { next: returnPath, tenantId: d.tenant_id, switchTo: tenantId, email },
    'none',
  );
}
