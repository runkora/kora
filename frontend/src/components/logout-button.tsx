'use client';

/**
 * @tier 2 — App-level shared. Calls the BFF POST /api/auth/logout endpoint,
 * revokes tokens server-side, clears the session cookie, then redirects to
 * /login. Used in the dashboard sidebar.
 * @promote-to-tier-1 if: the Orbit-specific auth endpoint is removed and a
 * generic sign-out button is needed outside the dashboard.
 */
import { Button, Icon } from '@orbit/terra';
import { useLogout } from '@orbit/lib/use-logout';

export function LogoutButton() {
  const { isLoggingOut, logout } = useLogout();

  return (
    <Button
      aria-label="Logout"
      variant="ghost"
      size="sm"
      onClick={() => void logout()}
      disabled={isLoggingOut}
    >
      {isLoggingOut ? <Icon name="loader" /> : <Icon name="log-out" />}
    </Button>
  );
}
