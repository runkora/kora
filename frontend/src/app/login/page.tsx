/**
 * Login page.
 *
 * Presents a single "Sign in" call-to-action. Clicking the button POSTs to
 * the BFF `/api/auth/login` route which redirects the browser to the OIDC
 * provider's authorization endpoint — no credentials are handled client-side.
 *
 * Supports an optional `?error` query parameter for displaying provider or
 * server error codes returned after a failed OIDC round-trip.
 * Supports an optional `?next` query parameter that is forwarded to the BFF
 * so the post-login redirect is preserved across the OIDC round-trip.
 */

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
  const actionUrl = next
    ? `/api/auth/login?next=${encodeURIComponent(next)}`
    : '/api/auth/login';

  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-gray-950 text-white">
      <div className="flex flex-col items-center gap-8 text-center">
        {/* Wordmark */}
        <div className="flex flex-col items-center gap-2">
          <h1 className="text-5xl font-bold tracking-tight">Orbit</h1>
          <p className="text-gray-400 text-lg">Control plane for autonomous AI agents.</p>
        </div>

        {/* Error message (shown only after a failed OIDC round-trip) */}
        {message && (
          <div
            role="alert"
            className="rounded-md border border-red-500/40 bg-red-950/40 px-4 py-3 text-sm text-red-300"
          >
            {message}
          </div>
        )}

        {/* Sign-in card */}
        <div className="flex flex-col items-center gap-4 rounded-xl border border-white/10 bg-white/5 px-10 py-8 shadow-xl backdrop-blur-sm">
          <p className="text-gray-300 text-sm">Sign in with your organization account.</p>

          <form method="POST" action={actionUrl}>
            <button
              type="submit"
              className="rounded-lg bg-white px-6 py-2.5 text-sm font-semibold text-gray-900 shadow transition hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-white/50"
            >
              Sign in
            </button>
          </form>
        </div>
      </div>
    </main>
  );
}
