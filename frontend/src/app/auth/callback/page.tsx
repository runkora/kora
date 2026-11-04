/**
 * /auth/callback — OIDC authorization code callback page.
 *
 * This Client Component is the registered `redirect_uri` for the browser-side
 * OIDC client. It runs entirely in the browser:
 *
 *   1. Reads the tenant OIDC config written to sessionStorage before
 *      `signinRedirect()` (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md sessionStorage handoff).
 *
 *   2. Calls `signinRedirectCallback()`, which completes PKCE from the browser
 *      and returns the `id_token` and the original state.
 *
 *   3. POSTs the ID token to `/api/auth/session` (BFF route), which exchanges
 *      it for a kora session and seals it into an HttpOnly cookie.
 *
 *   4. Redirects to `state.next` (validated relative path) or `/`.
 *
 * Tenant switching (docs/adr/2026-11-04-tenant-switching.md): when several
 * tenants match, the page shows a picker and re-posts the same ID token with
 * the chosen tenant. A switch carries its target in `state.switchTo`; a denied
 * switch keeps the current session, and a silent sign-in the provider cannot
 * complete falls back to an interactive sign-in with the same state.
 *
 * Other failures redirect to `/login?error=<code>`.
 */
'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { ErrorResponse } from 'oidc-client-ts';
import { Stack } from '@runkora/hades';

import {
  buildUserManager,
  TENANT_CONFIG_STORAGE_KEY,
  type TenantOidcConfig,
} from '@runkora/lib/oidc-client';
import { isSafeRedirectPath } from '@runkora/lib/routing';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import { redirectToSignIn } from '@runkora/lib/tenant-switch';
import type { SessionTenant } from '@runkora/server/auth-config';

import { SwitchDenied } from './_components/switch-denied';
import { TenantPicker } from './_components/tenant-picker';

interface Exchange {
  idToken: string;
  tenantId: string;
  next: string;
  switchTo?: string;
}

/** OIDC Core 3.1.2.6: the errors a `prompt=none` request returns when it needs the person. */
const SILENT_SIGNIN_ERRORS = new Set([
  'login_required',
  'interaction_required',
  'consent_required',
  'account_selection_required',
]);

type View = { kind: 'working' } | { kind: 'pick'; tenants: SessionTenant[] } | { kind: 'denied' };

export default function CallbackPage() {
  const router = useRouter();
  // Guard against StrictMode double-invocation — only run the exchange once.
  const exchanged = useRef(false);
  const exchange = useRef<Exchange | null>(null);
  const [view, setView] = useState<View>({ kind: 'working' });
  const [picking, setPicking] = useState<string | null>(null);

  // Posts the ID token to the BFF. `hint` is the tenant picked here; a switch
  // always sends its target.
  const seal = useCallback(
    async (ex: Exchange, hint?: string) => {
      try {
        const res = await fetch(ApiRoute.session.POST(), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id_token: ex.idToken,
            tenant_id: ex.tenantId,
            tenant_hint: hint ?? ex.switchTo,
            switch: ex.switchTo ? true : undefined,
          }),
        });
        const body = (await res.json().catch(() => ({}))) as {
          error?: string;
          tenants?: SessionTenant[];
        };
        if (res.ok) {
          // A full reload drops client state from the old tenant.
          if (ex.switchTo) window.location.replace(ex.next);
          else router.replace(ex.next);
          return;
        }
        if (res.status === 409 && body.tenants?.length) {
          setView({ kind: 'pick', tenants: body.tenants });
        } else if (res.status === 403 && body.error === 'tenant_switch_denied') {
          setView({ kind: 'denied' });
        } else {
          router.replace('/login?error=server_error');
        }
      } catch {
        router.replace('/login?error=server_error');
      }
    },
    [router],
  );

  useEffect(() => {
    if (exchanged.current) return;
    exchanged.current = true;

    void handleCallback();

    async function handleCallback() {
      try {
        // ── Step 1: recover tenant config from sessionStorage ──────────────
        //
        // The login page wrote this immediately before signinRedirect() so the
        // callback page can reconstruct an identical UserManager. Without this
        // the browser wouldn't know which IdP to call for the token exchange.
        const raw = sessionStorage.getItem(TENANT_CONFIG_STORAGE_KEY);
        if (!raw) {
          // No config found — either the user landed here directly (no login
          // flow was started in this tab) or sessionStorage was cleared.
          router.replace('/login?error=invalid_request');
          return;
        }

        let tenantConfig: TenantOidcConfig;
        try {
          const parsed: unknown = JSON.parse(raw);
          if (
            !parsed ||
            typeof parsed !== 'object' ||
            typeof (parsed as Record<string, unknown>).issuerUrl !== 'string' ||
            typeof (parsed as Record<string, unknown>).clientId !== 'string' ||
            typeof (parsed as Record<string, unknown>).tenantId !== 'string'
          ) {
            router.replace('/login?error=invalid_request');
            return;
          }
          tenantConfig = parsed as TenantOidcConfig;
        } catch {
          router.replace('/login?error=invalid_request');
          return;
        }

        // Remove now that we have it — keep sessionStorage tidy.
        sessionStorage.removeItem(TENANT_CONFIG_STORAGE_KEY);

        // ── Step 2: complete the PKCE exchange ─────────────────────────────
        const userManager = buildUserManager(tenantConfig);
        let user;
        try {
          user = await userManager.signinRedirectCallback();
        } catch (err) {
          // A silent switch the provider cannot complete without the person retries interactively.
          const s: Record<string, unknown> =
            err instanceof ErrorResponse &&
            SILENT_SIGNIN_ERRORS.has(err.error ?? '') &&
            err.state &&
            typeof err.state === 'object'
              ? (err.state as Record<string, unknown>)
              : {};
          if (
            typeof s.switchTo === 'string' &&
            typeof s.next === 'string' &&
            typeof s.tenantId === 'string'
          ) {
            await redirectToSignIn(tenantConfig, {
              next: s.next,
              tenantId: s.tenantId,
              switchTo: s.switchTo,
              email: typeof s.email === 'string' ? s.email : undefined,
            });
            return;
          }
          throw err;
        }

        const idToken = user.id_token;
        if (!idToken) {
          router.replace('/login?error=invalid_callback');
          return;
        }

        // ── Step 3: extract post-login destination and tenant_id from state ─
        //
        // oidc-client-ts restores the state object we passed to signinRedirect().
        // Both `next` and `tenantId` were encoded there by EmailSignInForm.
        const state =
          user.state && typeof user.state === 'object'
            ? (user.state as Record<string, unknown>)
            : {};

        const rawNext = typeof state.next === 'string' ? state.next : '/';
        const next = isSafeRedirectPath(rawNext) ? rawNext : '/';

        // tenant_id from the PKCE state is the authoritative source — it was
        // resolved by HRD and round-tripped through the IdP unchanged.
        // Reject outright if missing: a silent fallback to sessionStorage could
        // allow a compromised or manipulated flow to authenticate against the
        // wrong tenant.
        if (typeof state.tenantId !== 'string' || !state.tenantId) {
          router.replace('/login?error=invalid_callback');
          return;
        }
        const tenantId = state.tenantId;

        // ── Step 4: seal into BFF session ──────────────────────────────────
        exchange.current = {
          idToken,
          tenantId,
          next,
          switchTo:
            typeof state.switchTo === 'string' && state.switchTo ? state.switchTo : undefined,
        };
        await seal(exchange.current);
      } catch {
        router.replace('/login?error=server_error');
      }
    }
  }, [router, seal]);

  // The user sees nothing for the brief moment the exchange is in-flight.
  if (view.kind === 'working') return null;

  return (
    <Stack align="center" justify="center" minHeight="screen" padding="comfortable">
      {view.kind === 'pick' ? (
        <TenantPicker
          tenants={view.tenants}
          pickingId={picking}
          onPick={(tenantId) => {
            if (!exchange.current) return;
            setPicking(tenantId);
            void seal(exchange.current, tenantId).finally(() => setPicking(null));
          }}
        />
      ) : (
        <Stack maxWidth="card">
          <SwitchDenied
            onContinue={() => exchange.current && window.location.replace(exchange.current.next)}
          />
        </Stack>
      )}
    </Stack>
  );
}
