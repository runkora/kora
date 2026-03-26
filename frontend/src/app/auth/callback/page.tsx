/**
 * /auth/callback — OIDC authorization code callback page.
 *
 * This Client Component is the registered `redirect_uri` for the browser-side
 * OIDC client. It runs entirely in the browser:
 *
 *   1. Calls `signinRedirectCallback()` on the OIDC UserManager, which:
 *      - Reads `code` and `state` from the URL search params
 *      - Retrieves the PKCE verifier from sessionStorage (written by signinRedirect)
 *      - Exchanges the code at the OIDC token endpoint directly from the browser
 *      - Returns a `User` object containing the `id_token`
 *
 *   2. POSTs the `id_token` to `/api/auth/session` (BFF route) which:
 *      - Forwards to Go `POST /identity/session` with `grant_type: oidc_id_token`
 *      - Receives an Orbit platform JWT in return
 *      - Seals it into an HttpOnly `iron-session` cookie
 *
 *   3. Redirects to `state.next` (validated relative path) or `/`.
 *
 * Error handling: any failure redirects to `/login?error=<code>` so the user
 * always ends up on a recognisable page rather than a blank or broken view.
 */
'use client';

import { useEffect, useRef } from 'react';
import { useRouter } from 'next/navigation';

import { getUserManager } from '@/lib/oidc-client';
import { isSafeRedirectPath } from '@/shared/routes';

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
        const userManager = getUserManager();
        const user = await userManager.signinRedirectCallback();

        const idToken = user.id_token;
        if (!idToken) {
          router.replace('/login?error=invalid_callback');
          return;
        }

        // Extract the post-login destination from the OIDC state parameter.
        // oidc-client-ts restores the state object we passed to signinRedirect().
        const rawNext =
          user.state && typeof user.state === 'object' && 'next' in user.state
            ? String((user.state as { next: string }).next)
            : '/';
        const next = isSafeRedirectPath(rawNext) ? rawNext : '/';

        // Hand the id_token to the BFF. The BFF validates it against the Go
        // identity service and seals the resulting Orbit JWT into a session cookie.
        const res = await fetch('/api/auth/session', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id_token: idToken }),
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
