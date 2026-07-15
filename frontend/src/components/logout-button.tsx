'use client';

/**
 * @tier 2 — App-level shared. Calls the BFF POST /api/auth/logout endpoint,
 * revokes tokens server-side, clears the session cookie, then redirects to
 * /login. Used in the dashboard sidebar.
 * @promote-to-tier-1 if: the Orbit-specific auth endpoint is removed and a
 * generic sign-out button is needed outside the dashboard.
 */
import { useState } from 'react';
import { Button, Icon } from '@orbit/terra';
import { ApiRoute, usePrefix } from '@orbit/lib/routing';

export function LogoutButton() {
  const [pending, setPending] = useState(false);
  const prefix = usePrefix();

  async function handleLogout() {
    setPending(true);
    try {
      await fetch(ApiRoute.logout.POST(), { method: 'POST' });
    } finally {
      // Always navigate to /login — even if revocation fails the session cookie
      // is cleared server-side so the user is effectively signed out.
      window.location.href = `${prefix}/login`;
    }
  }

  return (
    <Button
      aria-label="Logout"
      variant="ghost"
      size="sm"
      onClick={() => void handleLogout()}
      disabled={pending}
    >
      {pending ? <Icon name="loader" /> : <Icon name="log-out" />}
    </Button>
  );
}
