import 'server-only';
import { createHmac, timingSafeEqual } from 'crypto';

/**
 * MCP OAuth consent CSRF protection.
 *
 * The consent decision (POST /api/mcp/authorize) mints an authorization code, so
 * it must only be reachable from a consent page this server rendered for this
 * user — never a cross-site auto-submit. We bind the decision to the render with
 * a stateless HMAC token: the consent page emits it as a hidden field, and the
 * decision handler re-derives and compares it. The HMAC key (BFF_SESSION_SECRET)
 * is server-only, so an attacker cannot forge a token for any parameters; binding
 * the user + the exact OAuth params also stops a token being replayed for a
 * different user or swapped onto different parameters.
 *
 * This is defense-in-depth on top of the SameSite=Lax session cookie (not sent on
 * cross-site POST) and the Sec-Fetch-Site check in the handler.
 */

interface ConsentTokenInput {
  /** A stable per-user session value (the human's email) — scopes the token. */
  userKey: string;
  clientId: string;
  redirectUri: string;
  codeChallenge: string;
}

function hmacKey(): string {
  const secret = process.env.BFF_SESSION_SECRET;
  if (!secret || secret.length < 32) {
    throw new Error('BFF_SESSION_SECRET must be set and at least 32 characters long.');
  }
  return secret;
}

/** mcpConsentToken derives the CSRF token for a consent request. */
export function mcpConsentToken(input: ConsentTokenInput): string {
  const message = [input.userKey, input.clientId, input.redirectUri, input.codeChallenge].join('\n');
  return createHmac('sha256', hmacKey()).update(message).digest('base64url');
}

/** consentTokenValid re-derives the expected token and compares it in constant time. */
export function consentTokenValid(submitted: string | null, input: ConsentTokenInput): boolean {
  if (!submitted) return false;
  const expected = mcpConsentToken(input);
  const a = Buffer.from(submitted);
  const b = Buffer.from(expected);
  if (a.length !== b.length) return false;
  return timingSafeEqual(a, b);
}
