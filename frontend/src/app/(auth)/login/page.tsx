/**
 * /login — OIDC sign-in entry point.
 *
 * Provider-agnostic: no username/password fields, no branded identity-provider
 * buttons. A single "Sign In" CTA triggers the browser-side OIDC client to
 * initiate an Authorization Code Flow with PKCE, redirecting the browser
 * directly to the configured identity provider.
 *
 * The post-login destination (`next`) is forwarded inside the OIDC `state`
 * parameter so the callback page can restore it after the provider round-trip.
 *
 * Centering is handled by (auth)/layout.tsx — this page renders only the
 * constrained card content. No sidebar or header chrome.
 *
 * Supports an optional `?error` query parameter for displaying provider or
 * server error codes returned after a failed OIDC round-trip.
 * Supports an optional `?next` query parameter forwarded to the OIDC state.
 */
import { redirect } from 'next/navigation';

import {
  Alert,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  Heading,
  Stack,
  Text,
} from '@orbit/terra';

import { getSession, isSessionValid } from '@orbit/server/session';
import { isSafeRedirectPath } from '@orbit/lib/routing';

import { EmailSignInForm } from './_components/email-sign-in-form';

interface LoginPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

const ERROR_MESSAGES: Record<string, string> = {
  server_error: 'An error occurred. Please try again.',
  invalid_callback: 'The sign-in request was invalid. Please try again.',
  invalid_request: 'The sign-in request was rejected. Please try again.',
  access_denied: 'Access was denied by the identity provider.',
  login_required: 'Your session expired. Please sign in again.',
};

function errorMessage(code: string | null): string | null {
  if (!code) return null;
  return ERROR_MESSAGES[code] ?? 'Sign-in failed. Please try again.';
}

export default async function LoginPage({ searchParams }: LoginPageProps) {
  const params = await searchParams;
  const errorCode = typeof params.error === 'string' ? params.error : null;
  const next = typeof params.next === 'string' ? params.next : '/';

  const session = await getSession();
  if (isSessionValid(session)) {
    redirect(isSafeRedirectPath(next) && next !== '/login' ? next : '/');
  }

  const message = errorMessage(errorCode);

  /*
   * Stack constrains the card to max-w-sm (384px) while remaining full-width
   * on narrow viewports via w-full. Horizontal padding prevents the card from
   * touching the screen edges on mobile. Centering is provided by (auth)/layout.tsx.
   */
  return (
    <Stack maxWidth="card">
      <Card>
        <CardHeader>
          {/* Eyebrow — product name, not a heading landmark */}
          <Heading variant="title" color="muted">
            Orbit
          </Heading>
          <CardDescription>
            Enter your work email to be directed to your organisation&apos;s identity provider.
          </CardDescription>
        </CardHeader>

        <CardContent>
          {/* Error banner — shown only after a failed OIDC round-trip */}
          {message && (
            <div className="mb-4">
              <Alert variant="error">
                <Text size="caption">{message}</Text>
              </Alert>
            </div>
          )}

          {/*
           * EmailSignInForm drives Home Realm Discovery (ADR-0031): the user
           * enters their work email, the BFF resolves the tenant's OIDC
           * configuration, and the browser is redirected to the correct IdP.
           * The `next` destination is encoded in the PKCE state so the
           * callback page can restore it after the provider round-trip.
           */}
          <EmailSignInForm next={next} />
        </CardContent>

        {/* CardFooter already applies border-t border-border-subtle */}
        <CardFooter compact justify="center">
          <Text size="caption" color="subtle" align="center">
            Single sign-on via OpenID Connect
          </Text>
        </CardFooter>
      </Card>
    </Stack>
  );
}
