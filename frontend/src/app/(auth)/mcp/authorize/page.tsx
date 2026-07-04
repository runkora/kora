import 'server-only';
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
import { config } from '@orbit/server/config';
import { mcpConsentToken } from '@orbit/server/mcp-consent';

import { ConsentActions } from './_components/consent-actions';

/**
 * /mcp/authorize — the OAuth 2.1 authorization endpoint (MCP profile), rendered
 * as an explicit consent screen. It is the AS metadata's authorization_endpoint.
 *
 * Flow: validate params → reuse the Orbit session (cold start → /login, query
 * preserved) → show the human which registered client is asking and that the
 * token acts as them → Approve/Deny. Approval POSTs to /api/mcp/authorize, which
 * mints the code and 302s back to the client's loopback redirect_uri. Nothing is
 * minted until the human approves.
 *
 * Security: the redirect_uri is validated as loopback (RFC 8252) before the page
 * renders — a non-loopback URI returns an error card, never a redirect.
 */
interface ConsentPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

function str(v: string | string[] | undefined): string {
  return typeof v === 'string' ? v : '';
}

function isLoopbackRedirect(raw: string): boolean {
  try {
    const u = new URL(raw);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
    return ['127.0.0.1', 'localhost', '::1', '[::1]'].includes(u.hostname);
  } catch {
    return false;
  }
}

function ConsentError({ title, message }: { title: string; message: string }) {
  return (
    <Stack maxWidth="card">
      <Card>
        <CardHeader>
          <Heading variant="section">{title}</Heading>
        </CardHeader>
        <CardContent>
          <Alert variant="error">{message}</Alert>
        </CardContent>
      </Card>
    </Stack>
  );
}

export default async function ConsentPage({ searchParams }: ConsentPageProps) {
  const params = await searchParams;
  const clientId = str(params.client_id);
  const redirectUri = str(params.redirect_uri);
  const codeChallenge = str(params.code_challenge);
  const codeChallengeMethod = str(params.code_challenge_method);
  const responseType = str(params.response_type);
  const state = str(params.state);
  const scope = str(params.scope);

  // Never render (or later redirect) for a non-loopback redirect_uri.
  if (!redirectUri || !isLoopbackRedirect(redirectUri)) {
    return (
      <ConsentError
        title="Invalid request"
        message="The redirect address is not a permitted loopback address."
      />
    );
  }
  if (responseType !== 'code' || !clientId || codeChallengeMethod !== 'S256' || !codeChallenge) {
    return (
      <ConsentError
        title="Invalid request"
        message="This authorization request is missing required PKCE parameters."
      />
    );
  }

  const session = await getSession();
  if (!isSessionValid(session)) {
    // Cold start: log in, then return here (full query preserved) to consent.
    const back = new URLSearchParams({
      response_type: 'code',
      client_id: clientId,
      redirect_uri: redirectUri,
      code_challenge: codeChallenge,
      code_challenge_method: 'S256',
    });
    if (state) back.set('state', state);
    if (scope) back.set('scope', scope);
    redirect(`/login?next=${encodeURIComponent(`/mcp/authorize?${back.toString()}`)}`);
  }

  // Name the application asking (registered via DCR).
  let clientName = '';
  let unknownClient = false;
  try {
    const res = await fetch(
      `${config.orbitApiUrl}/identity/oauth/clients/${encodeURIComponent(clientId)}`,
      { cache: 'no-store', headers: { Authorization: `Bearer ${session.accessToken}` } },
    );
    if (res.status === 404) {
      unknownClient = true;
    } else if (res.ok) {
      const body = (await res.json()) as { client_name?: string };
      clientName = body.client_name ?? '';
    }
  } catch {
    // Non-fatal — fall through with a generic name.
  }
  if (unknownClient) {
    return (
      <ConsentError
        title="Unknown application"
        message="This application is not registered with Orbit."
      />
    );
  }

  const appName = clientName || 'An application';
  const destination = new URL(redirectUri).host;

  // The decision POSTs to the issuing route; the OAuth params ride in the action
  // URL so no hidden inputs are needed. The activated button's name/value carries
  // approve vs deny.
  const action = `/api/mcp/authorize?${new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: redirectUri,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
    ...(state ? { state } : {}),
    ...(scope ? { scope } : {}),
  }).toString()}`;

  // CSRF: bind this decision to the render for this user + these exact params.
  const csrfToken = mcpConsentToken({
    userKey: session.user.email,
    clientId,
    redirectUri,
    codeChallenge,
  });

  return (
    <Stack maxWidth="card">
      <Card>
        <CardHeader>
          <Heading variant="section">Authorize {appName}</Heading>
          <CardDescription>{appName} wants to connect to Orbit MCP.</CardDescription>
        </CardHeader>
        <CardContent>
          <Stack gap="comfortable">
            <Text>
              It will act as{' '}
              <Text as="span" weight="heavy">
                {session.user.email}
              </Text>{' '}
              when calling Orbit MCP tools — limited to what your permissions already allow, and
              nothing more.
            </Text>
            <Text size="supporting" color="muted">
              Tokens are delivered to {destination} on this device.
            </Text>
          </Stack>
        </CardContent>
        <CardFooter>
          <ConsentActions action={action} csrfToken={csrfToken} />
        </CardFooter>
      </Card>
    </Stack>
  );
}
