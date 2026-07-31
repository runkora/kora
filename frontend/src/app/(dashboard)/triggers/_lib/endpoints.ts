/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the derivation is a Triggers routing fact, and
 * only trigger surfaces ever show an ingest endpoint.
 *
 * Where inbound webhooks arrive.
 *
 * The inbound path is owned by the source, so every surface that shows an
 * endpoint (wizard step, success screen, drawer, created-trigger result) is
 * showing the same derivation. Written once so a routing change is one edit
 * rather than four that drift.
 */

/** Shown before the browser can say what host it is on, e.g. during SSR. */
export const INGEST_HOST_FALLBACK = 'this cluster';

/**
 * The public host webhook senders post to.
 *
 * Webhooks are ingested on the API ingress (`api.<host>`), not the console's
 * own host: the orbit-api VirtualService routes `/api/v1/webhooks/` to
 * orbit-triggers, while the console host redirects to login.
 */
export function ingestHostFromWindow(): string {
  return typeof window === 'undefined' ? INGEST_HOST_FALLBACK : `api.${window.location.host}`;
}

/** The full URL a provider posts deliveries for one source to. */
export function endpointFor(ingestHost: string, sourceId: string): string {
  return `https://${ingestHost}/api/v1/webhooks/${sourceId}`;
}
