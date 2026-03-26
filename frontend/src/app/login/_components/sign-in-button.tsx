/**
 * SignInButton — client component.
 *
 * Calls `signinRedirect()` on the OIDC client when the user activates the
 * button. The `next` prop (a validated relative path) is forwarded in the OIDC
 * `state` parameter so the callback page can restore the post-login destination
 * after the provider round-trip.
 *
 * This must be a Client Component because it calls browser APIs
 * (sessionStorage via oidc-client-ts and window.location).
 */
'use client';

import { useState } from 'react';
import { Button, Text } from '@orbit/terra';

import { getUserManager } from '@/lib/oidc-client';

interface SignInButtonProps {
  /** Validated relative path to redirect to after sign-in. Defaults to '/'. */
  next?: string;
}

export function SignInButton({ next = '/' }: SignInButtonProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSignIn() {
    setLoading(true);
    setError(null);
    try {
      await getUserManager().signinRedirect({
        // Encode the post-login destination in OIDC state so the callback
        // page can retrieve it after the provider round-trip. oidc-client-ts
        // serialises this object and restores it in signinRedirectCallback().
        state: { next },
      });
      // signinRedirect() triggers a full-page navigation to the OIDC provider.
      // Code below this line is never reached in the happy path.
    } catch (err) {
      // Log the real error so it is visible in DevTools — never swallow it.
      console.error('[SignInButton] signinRedirect failed:', err);
      setLoading(false);
      // Show a user-friendly message inline rather than redirecting away.
      // Redirecting to /login?error=... from a client-side catch resets the
      // page and loses the original error, making failures very hard to debug.
      setError('Unable to reach the identity provider. Please try again.');
    }
  }

  return (
    <div className="space-y-3">
      {error && (
        // eslint-disable-next-line local/no-raw-div
        <div role="alert" className="rounded-md border border-red-500/40 bg-red-950/40 px-3 py-2">
          <Text size="caption" color="status-error">
            {error}
          </Text>
        </div>
      )}
      <Button type="button" shape="pill" fullWidth disabled={loading} onClick={handleSignIn}>
        {loading ? 'Redirecting…' : 'Sign In'}
      </Button>
    </div>
  );
}
