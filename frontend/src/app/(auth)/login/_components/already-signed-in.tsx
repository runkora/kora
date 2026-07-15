/**
 * AlreadySignedIn — shown on /login when the visitor already has a valid session.
 *
 * Displays a brief explanatory message and automatically redirects to the
 * dashboard after 3 seconds. A manual link lets the user navigate immediately
 * without waiting for the timer.
 */
'use client';

import { useEffect } from 'react';

import { Card, CardContent, CardFooter, CardHeader, Heading, Text } from '@orbit/terra';
import { AppLink } from '@orbit/components/links';
import { usePrefixedRouter } from '@orbit/lib/routing';

const REDIRECT_DELAY_MS = 3_000;

export function AlreadySignedIn() {
  const router = usePrefixedRouter();

  useEffect(() => {
    const timer = setTimeout(() => {
      router.replace('/');
    }, REDIRECT_DELAY_MS);

    return () => clearTimeout(timer);
  }, [router]);

  return (
    <Card>
      <CardHeader>
        <Heading variant="title" color="muted">
          Orbit
        </Heading>
      </CardHeader>

      <CardContent>
        <Text>
          You&apos;re already signed in. You&apos;ll be redirected to the dashboard in a few
          seconds.
        </Text>
      </CardContent>

      <CardFooter compact justify="center">
        <AppLink to="/" size="sm">
          Go to dashboard now
        </AppLink>
      </CardFooter>
    </Card>
  );
}
