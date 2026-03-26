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
 * Page variant "centered" gives a full-viewport, vertically and horizontally
 * centred layout with no sidebar or header chrome — the correct frame for
 * auth, onboarding, and error pages (see Page component, page.tsx).
 *
 * Supports an optional `?error` query parameter for displaying provider or
 * server error codes returned after a failed OIDC round-trip.
 * Supports an optional `?next` query parameter forwarded to the OIDC state.
 */
import { Page } from '@core/components/page';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  Heading,
  Stack,
  Text,
} from '@orbit/terra';

import { SignInButton } from './_components/sign-in-button';

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

  const message = errorMessage(errorCode);

  return (
    <Page variant="centered">
      {/*
       * Stack constrains the card to max-w-sm (384px) while remaining full-width
       * on narrow viewports via w-full. Horizontal padding prevents the card from
       * touching the screen edges on mobile.
       */}
      <Stack maxWidth="card">
        <Card>
          <CardHeader>
            {/* Eyebrow — product name, not a heading landmark */}
            <Heading variant="title" color="muted">
              Orbit
            </Heading>
            <CardDescription>
              You&apos;ll be redirected to your organisation&apos;s identity provider to complete
              sign-in.
            </CardDescription>
          </CardHeader>

          <CardContent>
            {/* Error banner — shown only after a failed OIDC round-trip */}
            {message && (
              // eslint-disable-next-line local/no-raw-div
              <div
                role="alert"
                className="mb-4 rounded-md border border-red-500/40 bg-red-950/40 px-3 py-2"
              >
                <Text size="caption" color="status-error">
                  {message}
                </Text>
              </div>
            )}

            {/*
             * SignInButton calls signinRedirect() on the browser-side OIDC
             * client. The `next` destination is encoded in the OIDC state so
             * the callback page can restore it after the provider round-trip.
             */}
            <SignInButton next={next} />
          </CardContent>

          {/* CardFooter already applies border-t border-border-subtle */}
          <CardFooter compact justify="center">
            <Text size="caption" color="subtle" align="center">
              Single sign-on via OpenID Connect
            </Text>
          </CardFooter>
        </Card>
      </Stack>
    </Page>
  );
}
