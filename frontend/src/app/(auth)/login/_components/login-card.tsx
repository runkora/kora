/**
 * @tier 3 — login-scoped. The sign-in card: a title, a short explanation, the
 * email form, and an error banner after a failed OIDC round-trip. The page
 * passes the error message and the post-login destination.
 */
import { Alert, Icon, Stack, Text } from '@orbit/terra';

import { AuthCardShell } from './auth-card-shell';
import { EmailSignInForm } from './email-sign-in-form';

interface LoginCardProps {
  /** Shown above the form after a failed sign-in. */
  message: string | null;
  /** Where to send the user after sign-in, carried in the OIDC state. */
  next: string;
}

export function LoginCard({ message, next }: LoginCardProps) {
  return (
    <AuthCardShell
      title="Sign in"
      description="Enter your email and we will take you to your organisation's identity provider."
      footer={
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="lock" size="2xs" color="subtle" />
          <Text size="caption" color="subtle">
            Single sign-on via OpenID Connect
          </Text>
        </Stack>
      }
    >
      {/* The banner and the form are one group, so they sit closer to each other than to the title. */}
      <Stack gap="comfortable">
        {message && (
          <Alert variant="error">
            <Text size="supporting">{message}</Text>
          </Alert>
        )}

        {/*
         * EmailSignInForm drives Home Realm Discovery (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md): the user
         * enters their email, the BFF resolves the tenant's OIDC
         * configuration, and the browser is redirected to the correct IdP.
         * The `next` destination is encoded in the PKCE state so the
         * callback page can restore it after the provider round-trip.
         */}
        <EmailSignInForm next={next} />
      </Stack>
    </AuthCardShell>
  );
}
