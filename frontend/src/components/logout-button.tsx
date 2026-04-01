'use client';

/**
 * LogoutButton
 *
 * Calls the BFF POST /api/auth/logout endpoint, which revokes both the access
 * and refresh tokens server-side before clearing the session cookie. The
 * browser is then redirected to /login regardless of the outcome so the user
 * is never left in an ambiguous auth state.
 */
import { useState } from 'react';
import { Button } from '@orbit/terra';
import { LoaderCircle, LogOut } from 'lucide-react';

export function LogoutButton() {
  const [pending, setPending] = useState(false);

  async function handleLogout() {
    setPending(true);
    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } finally {
      // Always navigate to /login — even if revocation fails the session cookie
      // is cleared server-side so the user is effectively signed out.
      window.location.href = '/login';
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
      {pending ? <LoaderCircle /> : <LogOut />}
    </Button>
  );
}
