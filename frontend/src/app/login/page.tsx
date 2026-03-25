/**
 * /auth — OIDC sign-in entry point.
 *
 * Provider-agnostic: no username/password fields, no branded identity-provider
 * buttons. A single "Continue" CTA redirects the browser to the BFF auth
 * initiation endpoint, which constructs the OIDC authorization request and
 * redirects to the configured identity provider.
 *
 * BFF route: /api/auth/login  (Next.js API route → Go identity service)
 *
 * Page variant "centered" gives a full-viewport, vertically and horizontally
 * centred layout with no sidebar or header chrome — the correct frame for
 * auth, onboarding, and error pages (see Page component, page.tsx).
 *
 * Supports an optional `?error` query parameter for displaying provider or
 * server error codes returned after a failed OIDC round-trip.
 * Supports an optional `?next` query parameter that is forwarded to the BFF
 * so the post-login redirect is preserved across the OIDC round-trip.
 */
import { Page } from '@core/components/page';
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Stack,
  Text,
} from '@orbit/terra';

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

interface LoginPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function LoginPage({ searchParams }: LoginPageProps) {
  const params = await searchParams;
  const errorCode = typeof params.error === 'string' ? params.error : null;
  const next = typeof params.next === 'string' ? params.next : '';

  const message = errorMessage(errorCode);

  // Build the action URL with a `next` parameter so the BFF can forward it.
  const actionUrl = next ? `/api/auth/login?next=${encodeURIComponent(next)}` : '/api/auth/login'; // TODO: replace inlines with route constants

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
            <Text size="caption" weight="strong" transform="uppercase" color="subtle">
              Orbit
            </Text>

            {/* h4 via CardTitle's Heading variant="label" — correct for a card */}
            <CardTitle>Sign in</CardTitle>

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
             * asChild renders the Button styles on the <a> tag directly so the
             * browser handles the navigation without JavaScript. fullWidth makes
             * the anchor fill the card content column.
             */}
            <Button asChild fullWidth>
              <a href={actionUrl}>Continue</a>
            </Button>
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
