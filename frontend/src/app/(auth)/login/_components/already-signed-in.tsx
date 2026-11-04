/**
 * AlreadySignedIn — shown on /login when the visitor already has a valid session.
 *
 * Displays a brief explanatory message and automatically redirects to the
 * dashboard after 3 seconds. A manual link lets the user navigate immediately
 * without waiting for the timer.
 */
'use client';

import { useEffect } from 'react';

import { useRouter } from 'next/navigation';

import { AppLink } from '@runkora/components/links';

import { AuthCardShell } from './auth-card-shell';

const REDIRECT_DELAY_MS = 3_000;

export function AlreadySignedIn() {
  const router = useRouter();

  useEffect(() => {
    const timer = setTimeout(() => {
      router.replace('/');
    }, REDIRECT_DELAY_MS);

    return () => clearTimeout(timer);
  }, [router]);

  return (
    <AuthCardShell
      title="You're signed in"
      description="You will be redirected to the dashboard in a few seconds."
      footer={
        <AppLink to="/" size="sm">
          Go to dashboard now
        </AppLink>
      }
    />
  );
}
