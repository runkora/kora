/**
 * E2E baseline — /login page.
 *
 * docs/adr/2026-04-01-browser-auth-flow-and-bff-session-architecture.md Category A constraint: no live Go backend is required.
 * All API calls are intercepted via page.route() — the Next.js server is
 * the only real process under test.
 *
 * Coverage:
 *   1. Login page renders the email input form
 *   2. Empty submit shows validation error
 *   3. Invalid email shows validation error
 *   4. 404 from discover shows a field-level "no org" error
 *   5. Network error from discover shows a banner error (catch path)
 *   6. Successful discover triggers an OIDC redirect
 *   7. ?error=server_error renders the error banner
 */
import { test, expect, type Route } from '@playwright/test';

// ── Shared fixture data ────────────────────────────────────────────────────────

const WORK_EMAIL = 'alice@acme.example.com';

const DISCOVER_RESPONSE = {
  tenant_id: 'tenant-acme',
  issuer_url: 'https://idp.acme.example.com',
  client_id: 'orbit-acme',
  scopes: ['openid', 'profile', 'email'],
};

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Intercept the BFF discover call and respond with a given payload / status. */
async function mockDiscover(
  page: import('@playwright/test').Page,
  status: number,
  body?: object,
): Promise<void> {
  await page.route('**/api/auth/discover*', (route: Route) => {
    void route.fulfill({
      status,
      contentType: 'application/json',
      body: body !== undefined ? JSON.stringify(body) : '',
    });
  });
}

/**
 * Intercept the OIDC discovery document fetch made by oidc-client-ts.
 *
 * Before building the authorization URL, oidc-client-ts fetches
 * `${issuer}/.well-known/openid-configuration` as a cross-origin fetch()
 * from `http://localhost:3000` to `https://idp.acme.example.com`.
 *
 * Playwright's route.fulfill() MUST include CORS response headers or the
 * browser's same-origin policy will block the response and signinRedirect()
 * will throw — which triggers the catch block and shows the "Unable to reach
 * the identity provider" banner instead of navigating to the IdP.
 *
 * Registration order matters: Playwright uses LIFO matching, so this helper
 * must be registered AFTER mockIdpAuth so it has higher priority and handles
 * the /.well-known/openid-configuration request before the broader
 * 'https://idp.acme.example.com/**' glob can match it.
 */
async function mockOidcDiscovery(page: import('@playwright/test').Page): Promise<void> {
  await page.route(
    'https://idp.acme.example.com/.well-known/openid-configuration',
    (route: Route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'GET, OPTIONS',
        },
        body: JSON.stringify({
          issuer: DISCOVER_RESPONSE.issuer_url,
          authorization_endpoint: `${DISCOVER_RESPONSE.issuer_url}/auth`,
          token_endpoint: `${DISCOVER_RESPONSE.issuer_url}/token`,
          jwks_uri: `${DISCOVER_RESPONSE.issuer_url}/jwks`,
          response_types_supported: ['code'],
          subject_types_supported: ['public'],
          id_token_signing_alg_values_supported: ['RS256'],
        }),
      });
    },
  );
}

/**
 * Intercept the IdP authorization endpoint so that Playwright can resolve and
 * load the navigation target.  oidc-client-ts performs a full-page redirect to
 * `${issuer_url}/auth?...`; without this the browser gets a DNS error because
 * `idp.acme.example.com` is not a real host.
 *
 * Register this BEFORE mockOidcDiscovery — Playwright uses LIFO matching, so
 * mockOidcDiscovery (registered last) wins for /.well-known/openid-configuration
 * and this handler only catches the remaining authorization redirect.
 */
async function mockIdpAuth(page: import('@playwright/test').Page): Promise<void> {
  await page.route('https://idp.acme.example.com/**', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'text/html',
      headers: { 'Access-Control-Allow-Origin': '*' },
      body: '<html><body>IdP mock</body></html>',
    });
  });
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('/login', () => {
  test('renders the email sign-in form', async ({ page }) => {
    await page.goto('/login');

    // Hades Card heading: "kora"
    await expect(page.getByRole('heading', { name: 'kora' })).toBeVisible();

    // Email input and submit button
    const emailInput = page.getByLabel('Email');
    await expect(emailInput).toBeVisible();
    await expect(emailInput).toHaveAttribute('type', 'email');

    await expect(page.getByRole('button', { name: 'Continue' })).toBeVisible();
  });

  test('shows a validation error when submitting an empty email', async ({ page }) => {
    await page.goto('/login');

    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText('Please enter your email address.')).toBeVisible();
  });

  test('shows a validation error for an invalid email (no @)', async ({ page }) => {
    await page.goto('/login');

    await page.getByLabel('Email').fill('notanemail');
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText('Please enter a valid email address.')).toBeVisible();
  });

  test('shows a field error when discover returns 404', async ({ page }) => {
    await mockDiscover(page, 404);
    await page.goto('/login');

    await page.getByLabel('Email').fill('unknown@unknown.example.com');
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(
      page.getByText('No organisation found for that email domain. Contact your administrator.'),
    ).toBeVisible();
  });

  test('shows a banner error when the discover request is aborted (network failure)', async ({
    page,
  }) => {
    // Abort the request — fetch() throws, so the catch block runs and sets:
    // "Unable to reach the identity provider. Please try again."
    // (A non-ok response would instead call setBannerError with
    //  "Unable to look up your organisation. Please try again." via the !res.ok branch.)
    await page.route('**/api/auth/discover*', (route: Route) => void route.abort());
    await page.goto('/login');

    await page.getByLabel('Email').fill(WORK_EMAIL);
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(
      page.getByText('Unable to reach the identity provider. Please try again.'),
    ).toBeVisible();
  });

  test('triggers an OIDC redirect on successful discover', async ({ page }) => {
    // Respond to the BFF discover call with a valid tenant config.
    await mockDiscover(page, 200, DISCOVER_RESPONSE);

    // Register handlers in reverse priority order — Playwright uses LIFO matching,
    // so the handler registered LAST has the highest priority.
    //
    // mockIdpAuth  (registered first)  — catches authorization redirect /auth?...
    // mockOidcDiscovery (registered last) — wins over mockIdpAuth for the more
    //   specific /.well-known/openid-configuration path, ensuring oidc-client-ts
    //   receives JSON (not HTML) and can build the authorization URL.
    await mockIdpAuth(page);
    await mockOidcDiscovery(page);

    await page.goto('/login');
    await page.getByLabel('Email').fill(WORK_EMAIL);
    await page.getByRole('button', { name: 'Continue' }).click();

    // Wait for the full-page navigation to the mocked IdP.  Verifying the
    // exact OIDC query parameters is an oidc-client-ts implementation detail
    // outside kora's responsibility.
    await page.waitForURL(/^https?:\/\/idp\.acme\.example\.com\//, { timeout: 10_000 });
    expect(page.url()).toContain('idp.acme.example.com');
  });

  test('renders the error banner when ?error=server_error is present', async ({ page }) => {
    await page.goto('/login?error=server_error');

    // Use .filter() to avoid matching the Next.js route announcer (also role="alert").
    const errorBanner = page.getByRole('alert').filter({ hasText: 'An error occurred' });
    await expect(errorBanner).toBeVisible();
    await expect(page.getByText('An error occurred. Please try again.')).toBeVisible();
  });
});
