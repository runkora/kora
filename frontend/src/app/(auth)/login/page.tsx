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

import { getSession, isSessionValid } from '@orbit/server/session';
import { isSafeRedirectPath } from '@orbit/lib/routing';

import { LoginCard } from './_components/login-card';

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

  return <LoginCard message={message} next={next} />;
}
