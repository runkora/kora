/**
 * EmailSignInForm — two-step Home Realm Discovery sign-in form.
 *
 * Step 1 — Email input:
 *   The user types their work email address and presses "Continue".
 *
 * Step 2 — Discover & redirect:
 *   The form calls the BFF discover endpoint (`GET /api/auth/discover?email=...`)
 *   which proxies to the Go identity service. On success the resolved tenant
 *   OIDC configuration is persisted to sessionStorage so the callback page can
 *   reconstruct the same `UserManager` after the provider round-trip, then the
 *   browser is redirected to the IdP via `signinRedirect()`.
 *
 * The `next` destination and `tenantId` are encoded in the OIDC `state`
 * parameter so the callback page can restore them after the round-trip without
 * any server round-trip.
 *
 * See ADR-0031 for the full design rationale.
 */
'use client';

import { useState } from 'react';
import { Alert, Button, Field, FieldError, Form, Input, Label, Text } from '@orbit/terra';

import {
  buildUserManager,
  TENANT_CONFIG_STORAGE_KEY,
  type TenantOidcConfig,
} from '@orbit/lib/oidc-client';
import { ApiRoute } from '@orbit/lib/routing/api-routes';

// ── Types ─────────────────────────────────────────────────────────────────────

interface EmailSignInFormProps {
  /** Validated relative path to redirect to after sign-in. Defaults to '/'. */
  next?: string;
}

/** Shape returned by `GET /api/auth/discover`. Mirrors `TenantDiscovery` in the Go service. */
interface DiscoverResponse {
  tenant_id: string;
  issuer_url: string;
  client_id: string;
  scopes: string[];
}

// ── Component ─────────────────────────────────────────────────────────────────

export function EmailSignInForm({ next = '/' }: EmailSignInFormProps) {
  const [email, setEmail] = useState('');
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [bannerError, setBannerError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFieldError(null);
    setBannerError(null);

    const trimmed = email.trim();
    if (!trimmed) {
      setFieldError('Please enter your work email address.');
      return;
    }
    if (!trimmed.includes('@')) {
      setFieldError('Please enter a valid email address.');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(ApiRoute.discover.GET(trimmed), {
        method: 'GET',
        headers: { Accept: 'application/json' },
      });

      if (res.status === 404) {
        setFieldError('No organisation found for that email domain. Contact your administrator.');
        setLoading(false);
        return;
      }

      if (!res.ok) {
        setBannerError('Unable to look up your organisation. Please try again.');
        setLoading(false);
        return;
      }

      const data = (await res.json()) as DiscoverResponse;

      const tenantConfig: TenantOidcConfig = {
        tenantId: data.tenant_id,
        issuerUrl: data.issuer_url,
        clientId: data.client_id,
        scopes: data.scopes,
      };

      // Persist the resolved tenant config so the callback page can reconstruct
      // the same UserManager after the IdP round-trip — sessionStorage is
      // tab-scoped and cleared when the tab is closed.
      try {
        sessionStorage.setItem(TENANT_CONFIG_STORAGE_KEY, JSON.stringify(tenantConfig));
      } catch {
        setBannerError(
          'Unable to store session data. Check that your browser allows sessionStorage.',
        );
        setLoading(false);
        return;
      }

      const userManager = buildUserManager(tenantConfig);

      // Thread tenant_id and next through the PKCE state parameter. The
      // callback page reads these from `user.state` after signinRedirectCallback().
      await userManager.signinRedirect({
        state: { next, tenantId: tenantConfig.tenantId },
      });

      // Full-page navigation to the IdP is in flight — code below is unreachable.
    } catch (err) {
      console.error('[EmailSignInForm] sign-in initiation failed:', err);
      setBannerError('Unable to reach the identity provider. Please try again.');
      setLoading(false);
    }
  }

  return (
    <Form onSubmit={handleSubmit} noValidate gap="comfortable">
      {bannerError && (
        <Alert variant="error">
          <Text size="caption">{bannerError}</Text>
        </Alert>
      )}

      <Field>
        <Label htmlFor="email">Work email</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          autoFocus
          placeholder="you@yourcompany.com"
          value={email}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setEmail(e.target.value)}
          disabled={loading}
          aria-describedby={fieldError ? 'email-error' : undefined}
          aria-invalid={!!fieldError}
        />
        {fieldError && <FieldError id="email-error">{fieldError}</FieldError>}
      </Field>

      <Button type="submit" shape="pill" fullWidth disabled={loading}>
        {loading ? 'Looking up your organisation…' : 'Continue'}
      </Button>
    </Form>
  );
}
