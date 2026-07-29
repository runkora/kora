import 'server-only';
import { redirect } from 'next/navigation';

import { Alert, Card, CardContent, CardHeader, Heading, Stack } from '@orbit/terra';

import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';
import { mcpConsentToken } from '@orbit/server/mcp-consent';
import { listGrantedTools, listMcpServers, listMcpTools } from '@orbit/server/mcp-client';

import { ConsentFlow, type ConsentDomain, type ConsentServer } from './_components/consent-flow';

/**
 * /mcp/authorize — the OAuth 2.1 authorization endpoint (MCP profile), rendered
 * as an explicit consent screen (the AS metadata's authorization_endpoint).
 *
 * Validates params → reuses the Orbit session (cold start → /login, query
 * preserved) → shows the requesting client, the human, and — for the server this
 * consent is for (from the RFC 8707 `resource`) — the exact tools being granted.
 * The client component owns the approve/deny + result states; nothing is minted
 * until the human approves. redirect_uri must be either a loopback address
 * (RFC 8252, native/DCR clients) or exactly one of the client's pre-registered
 * redirect_uris (OAuthClient CR, hosted-app clients) — checked once the client
 * lookup below resolves; a URI that is neither returns an error, never a redirect.
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

/** Structural check only (parses as http/https) — not a security gate. The real
 * gate is isLoopbackRedirect() OR exact membership in the client's registered
 * redirect_uris, checked once the client lookup below resolves. */
function isWellFormedRedirect(raw: string): boolean {
  try {
    const u = new URL(raw);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

/** The server slug this consent is for, from the RFC 8707 resource indicator. */
function serverFromResource(resource: string): string {
  try {
    const parts = new URL(resource).pathname.split('/').filter(Boolean);
    const i = parts.indexOf('mcp');
    return i >= 0 && parts[i + 1] ? parts[i + 1] : '';
  } catch {
    return '';
  }
}

/**
 * The public gateway endpoint the client connects to — the resource itself, with
 * the dev-only ingress port stripped. Never the internal upstream (svc DNS): that
 * would leak cluster topology and isn't the address the user talks to.
 */
function publicEndpoint(resource: string): string {
  try {
    const u = new URL(resource);
    u.port = '';
    return u.toString();
  } catch {
    return resource;
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
  const resource = str(params.resource);

  if (!redirectUri || !isWellFormedRedirect(redirectUri)) {
    return (
      <ConsentError title="Invalid request" message="A valid redirect address is required." />
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
    const back = new URLSearchParams({
      response_type: 'code',
      client_id: clientId,
      redirect_uri: redirectUri,
      code_challenge: codeChallenge,
      code_challenge_method: 'S256',
    });
    if (state) back.set('state', state);
    if (scope) back.set('scope', scope);
    if (resource) back.set('resource', resource);
    redirect(`/login?next=${encodeURIComponent(`/mcp/authorize?${back.toString()}`)}`);
  }

  // Name the application asking (registered via DCR or a pre-registered
  // OAuthClient CR), and — since redirectUri only passed a structural check
  // above — confirm it's one this client actually registered.
  let clientName = '';
  let unknownClient = false;
  let clientRedirectUris: string[] = [];
  try {
    const res = await fetch(
      `${config.orbitApiUrl}/identity/oauth/clients/${encodeURIComponent(clientId)}`,
      { cache: 'no-store', headers: { Authorization: `Bearer ${session.accessToken}` } },
    );
    if (res.status === 404) {
      unknownClient = true;
    } else if (res.ok) {
      const body = (await res.json()) as { client_name?: string; redirect_uris?: string[] };
      clientName = body.client_name ?? '';
      clientRedirectUris = body.redirect_uris ?? [];
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
  // The real gate: loopback URIs are always allowed (RFC 8252 native/DCR
  // clients); anything else must exactly match a redirect_uri this client
  // pre-registered (OAuthClient CR, hosted-app clients).
  if (!isLoopbackRedirect(redirectUri) && !clientRedirectUris.includes(redirectUri)) {
    return (
      <ConsentError
        title="Invalid request"
        message="The redirect address is not registered for this application."
      />
    );
  }

  // Tool surface: for the server this consent targets, show its tools grouped by
  // domain, marking which the human holds grants for.
  const serverSlug = serverFromResource(resource);
  let server: ConsentServer | null = null;
  let domains: ConsentDomain[] = [];
  let grantedCount = 0;
  let totalCount = 0;
  if (serverSlug) {
    const [servers, tools, granted] = await Promise.all([
      listMcpServers(session.accessToken).catch(() => []),
      listMcpTools(session.accessToken).catch(() => []),
      listGrantedTools(session.accessToken, serverSlug),
    ]);
    const meta = servers.find((s) => s.name === serverSlug);
    server = {
      name: meta?.name ?? serverSlug,
      displayName: meta?.display_name || serverSlug,
      // The public gateway URL the client connects to — not the internal upstream.
      endpoint: publicEndpoint(resource),
    };
    const grantedSet = new Set(granted);
    const byDomain = new Map<string, ConsentDomain>();
    for (const t of tools.filter((x) => x.server_slug === serverSlug)) {
      const domain = t.tool_name.split('.')[0] || t.tool_name;
      const isGranted = grantedSet.has(t.tool_name);
      totalCount += 1;
      if (isGranted) grantedCount += 1;
      const group = byDomain.get(domain) ?? {
        name: domain,
        scope: `mcp:${serverSlug}:${domain}:use`,
        tools: [],
      };
      group.tools.push({ name: t.tool_name, scope: t.scope, granted: isGranted });
      byDomain.set(domain, group);
    }
    domains = [...byDomain.values()].sort((a, b) => a.name.localeCompare(b.name));
  }

  const techRows = [
    { k: 'response_type', v: 'code' },
    { k: 'client_id', v: clientId },
    { k: 'redirect_uri', v: redirectUri, accent: true },
    { k: 'code_challenge', v: `${codeChallenge.slice(0, 16)}… · S256` },
    { k: 'scope', v: scope || 'mcp:invoke' },
  ];

  const action = `/api/mcp/authorize?${new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: redirectUri,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
    ...(state ? { state } : {}),
    ...(scope ? { scope } : {}),
  }).toString()}`;

  const csrfToken = mcpConsentToken({
    userKey: session.user.email,
    clientId,
    redirectUri,
    codeChallenge,
  });

  return (
    <ConsentFlow
      clientName={clientName || 'An application'}
      userEmail={session.user.email}
      server={server}
      domains={domains}
      grantedCount={grantedCount}
      totalCount={totalCount}
      techRows={techRows}
      actionUrl={action}
      csrfToken={csrfToken}
    />
  );
}
