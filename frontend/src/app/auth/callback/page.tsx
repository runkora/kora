/**
 * /auth/callback — OIDC authorization code callback page.
 *
 * This Client Component is the registered `redirect_uri` for the browser-side
 * OIDC client. It runs entirely in the browser:
 *
 *   1. Reads the tenant OIDC config written to sessionStorage by the login page
 *      immediately before `signinRedirect()` (ADR-0031 sessionStorage handoff).
 *
 *   2. Calls `signinRedirectCallback()` on a reconstructed UserManager, which:
 *      - Reads `code` and `state` from the URL search params
 *      - Retrieves the PKCE verifier from sessionStorage (written by signinRedirect)
 *      - Exchanges the code at the OIDC token endpoint directly from the browser
 *      - Returns a `User` object containing the `id_token` and the original state
 *
 *   3. POSTs `{ id_token, tenant_id }` to `/api/auth/session` (BFF route) which:
 *      - Forwards to Go `POST /identity/session` with `grant_type: oidc_id_token`
 *      - Receives an Orbit platform JWT in return
 *      - Seals it into an HttpOnly `iron-session` cookie with the backend's TTL
 *
 *   4. Redirects to `state.next` (validated relative path) or `/`.
 *
 * Error handling: any failure redirects to `/login?error=<code>` so the user
 * always ends up on a recognisable page rather than a blank or broken view.
 */
'use client';

import { useEffect, useRef } from 'react';
import { useRouter } from 'next/navigation';

import {
  buildUserManager,
  TENANT_CONFIG_STORAGE_KEY,
  type TenantOidcConfig,
} from '@orbit/lib/oidc-client';
import { isSafeRedirectPath } from '@orbit/lib/routing';

export default function CallbackPage() {
  const router = useRouter();
  // Guard against StrictMode double-invocation — only run the exchange once.
  const exchanged = useRef(false);

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
        const user = await userManager.signinRedirectCallback();

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
        const res = await fetch('/api/auth/session', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id_token: idToken, tenant_id: tenantId }),
        });

        if (!res.ok) {
          router.replace('/login?error=server_error');
          return;
        }

        router.replace(next);
      } catch {
        router.replace('/login?error=server_error');
      }
    }
  }, [router]);

  // The user sees this only for the brief moment the exchange is in-flight.
  return null;
}
