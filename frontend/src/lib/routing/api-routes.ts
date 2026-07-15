/**
 * api-routes.ts
 *
 * Single source of truth for every BFF (Backend-for-Frontend) API route in
 * the Orbit app.
 *
 * Each entry is keyed by route name, then by HTTP method. Methods are
 * functions so that routes with optional query parameters can be built
 * type-safely at the call site rather than via string concatenation.
 *
 * Add a new entry here when adding a new BFF route handler under
 * `src/app/api/`. Removing an entry will produce a compile error at every
 * call site that references the deleted route.
 *
 * @example
 *   import { ApiRoute } from '@orbit/lib/routing';
 *
 *   // Without parameter
 *   fetch(ApiRoute.logout.POST(), { method: 'POST' });
 *
 *   // With body
 *   fetch(ApiRoute.session.POST(), { method: 'POST', body: JSON.stringify({ id_token }) });
 */

import { getClientDevEnvPrefix } from './devenv-prefix';

// ── Base segments ─────────────────────────────────────────────────────────────

/**
 * Every BFF route this app serves lives under the current page's dev-
 * environment prefix (e.g. "/abc123/api/...") when running inside one, and
 * bare "/api/..." otherwise. Computed at call time (never cached at module
 * load) since the prefix is only known once PrefixProvider has rendered
 * client-side (prefix-context.tsx) — see devenv-prefix.ts.
 */
function apiBase(): string {
  return getClientDevEnvPrefix() + '/api';
}
function authBase(): string {
  return apiBase() + '/auth';
}

// ── Route registry ────────────────────────────────────────────────────────────

export const ApiRoute = {
  /**
   * `/api/health`
   *
   * Reachable without a session — used by monitoring and load-balancer health
   * checks that arrive through the Istio gateway.
   */
  health: {
    GET: (): string => `${apiBase()}/health`,
  },

  /**
   * `/api/identity/health`
   *
   * Proxies the identity service health check server-side so the browser never
   * communicates with the Go API directly. Displayed by the HealthStatus
   * component on the dashboard.
   */
  identityHealth: {
    GET: (): string => `${apiBase()}/identity/health`,
  },

  /**
   * `/api/identity/users`
   *
   * Returns all users in the caller's tenant. Does not require the
   * analytics scope — safe to call from admin pages.
   */
  identityUsers: {
    GET: (): string => `${apiBase()}/identity/users`,
  },

  /**
   * `/api/auth/logout`
   *
   * Revokes the Orbit tokens server-side (best-effort) and destroys the
   * iron-session cookie, then redirects to `/login`.
   */
  logout: {
    POST: (): string => `${authBase()}/logout`,
  },

  /**
   * `/api/auth/session`
   *
   * POST — called by the OIDC callback page with `{ id_token }` to exchange
   *        the provider id_token for an Orbit session cookie.
   *
   * GET  — returns the public (non-sensitive) portion of the current session
   *        for client components that need to know who is logged in.
   */
  session: {
    POST: (): string => `${authBase()}/session`,
    GET: (): string => `${authBase()}/session`,
  },

  /**
   * `/api/auth/discover?email=user@example.com`
   *
   * GET — Home Realm Discovery (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md). Resolves the OIDC tenant config
   *       for a given email address by matching the email domain against
   *       registered tenant email domains.
   *
   * Returns `{ tenant_id, issuer_url, client_id, scopes }` — never the client secret.
   */
  discover: {
    /**
     * The bare path without query parameters — use this in `PUBLIC_PATHS`
     * because the middleware matches `pathname` (no query string) against the
     * set. `GET(email)` cannot be used there since it always includes `?email=`.
     */
    PATH: (): string => `${authBase()}/discover`,
    GET: (email: string): string =>
      `${authBase()}/discover?email=${encodeURIComponent(email)}`,
  },

  /**
   * `/api/models`
   *
   * GET — proxies to the Go inference gateway `GET /v1/models` and returns
   *       the full list of available models. Requires an authenticated session.
   */
  models: {
    GET: (): string => `${apiBase()}/models`,
  },

  /**
   * `/api/policy/stats`
   *
   * GET — proxies to the policy-service `GET /api/v1/policy/stats`.
   */
  policyStats: {
    GET: (): string => `${apiBase()}/policy/stats`,
  },

  /**
   * `/api/policy/grants`
   *
   * GET  — list grants for the caller's tenant.
   * POST — create a new grant.
   */
  policyGrants: {
    GET: (): string => `${apiBase()}/policy/grants`,
    POST: (): string => `${apiBase()}/policy/grants`,
  },

  /**
   * `/api/policy/grants/[id]`
   *
   * DELETE — revoke a grant by ID.
   */
  policyGrant: {
    DELETE: (id: string): string =>
      `${apiBase()}/policy/grants/${id}`,
  },

  /**
   * `/api/policy/roles`
   *
   * GET — list role→scope mappings for the caller's tenant.
   * PUT — create or replace a role→scope mapping.
   */
  policyRoles: {
    GET: (): string => `${apiBase()}/policy/roles`,
    PUT: (): string => `${apiBase()}/policy/roles`,
  },

  /**
   * `/api/policy/roles/[role]`
   *
   * DELETE — remove a role→scope mapping.
   */
  policyRole: {
    DELETE: (role: string): string =>
      `${apiBase()}/policy/roles/${encodeURIComponent(role)}`,
  },

  /**
   * `/api/policy/vocabulary`
   *
   * GET — list scope vocabulary with optional usage counts.
   */
  policyVocabulary: {
    GET: (): string => `${apiBase()}/policy/vocabulary`,
  },

  /**
   * `/api/policy/routes`
   *
   * GET — list all registered route→scope mappings.
   */
  policyRoutes: {
    GET: (): string => `${apiBase()}/policy/routes`,
  },

  /**
   * `/api/policy/evaluate/explain`
   *
   * GET — scope explain for a principal (proxies to policy-gateway).
   */
  policyExplain: {
    GET: (): string => `${apiBase()}/policy/evaluate/explain`,
  },

  /**
   * `/api/policy/evaluate/simulate`
   *
   * POST — simulate a policy check (proxies to policy-gateway).
   */
  policySimulate: {
    POST: (): string =>
      `${apiBase()}/policy/evaluate/simulate`,
  },

  /**
   * `/api/usage`
   *
   * GET — proxies to the Go inference gateway `GET /v1/usage` and returns
   *       inference usage records for the authenticated user. Accepts an
   *       optional `key` query param to filter by a specific API key (PAT UUID)
   *       and an optional `cursor` for cursor-based pagination.
   */
  usage: {
    GET: (params?: { key?: string; cursor?: string }): string => {
      const qs = new URLSearchParams();
      if (params?.key) qs.set('key', params.key);
      if (params?.cursor) qs.set('cursor', params.cursor);
      const q = qs.toString();
      return q ? `${apiBase()}/usage?${q}` : `${apiBase()}/usage`;
    },
  },

  /**
   * `/api/analytics/me`
   *
   * GET — proxies to the Go inference gateway `GET /v1/analytics/me` and returns
   *       personal aggregated analytics for the authenticated principal.
   *       Accepts optional `days` (7 | 30 | 90), `granularity`, and `token_id` params.
   */
  analyticsMe: {
    GET: (params?: { days?: number; granularity?: string; tokenId?: string }): string => {
      const qs = new URLSearchParams();
      if (params?.days !== undefined) qs.set('days', String(params.days));
      if (params?.granularity) qs.set('granularity', params.granularity);
      if (params?.tokenId) qs.set('token_id', params.tokenId);
      const q = qs.toString();
      return q ? `${apiBase()}/analytics/me?${q}` : `${apiBase()}/analytics/me`;
    },
  },

  /**
   * `/api/analytics`
   *
   * GET — proxies to the Go inference gateway `GET /v1/analytics` and returns
   *       tenant-wide aggregated analytics. Requires `platform:analytics:read` scope.
   *       Accepts optional `days` (7 | 30 | 90), `granularity`, and `model_id` params.
   */
  analytics: {
    GET: (params?: { days?: number; granularity?: string; modelId?: string }): string => {
      const qs = new URLSearchParams();
      if (params?.days !== undefined) qs.set('days', String(params.days));
      if (params?.granularity) qs.set('granularity', params.granularity);
      if (params?.modelId) qs.set('model_id', params.modelId);
      const q = qs.toString();
      return q ? `${apiBase()}/analytics?${q}` : `${apiBase()}/analytics`;
    },
  },
  /**
   * `/api/analytics/principals`
   *
   * GET — returns per-principal usage aggregates enriched with identity data.
   *       BFF orchestrates inference gateway + identity service.
   *       Requires `platform:analytics:read` scope.
   *       Accepts optional `days` (7 | 30 | 90), `page`, `limit`, `sort_by`,
   *       and `order` params.
   */
  analyticsPrincipals: {
    GET: (params?: {
      days?: number;
      page?: number;
      limit?: number;
      sort_by?: string;
      order?: string;
    }): string => {
      const qs = new URLSearchParams();
      if (params?.days !== undefined) qs.set('days', String(params.days));
      if (params?.page !== undefined) qs.set('page', String(params.page));
      if (params?.limit !== undefined) qs.set('limit', String(params.limit));
      if (params?.sort_by !== undefined) qs.set('sort_by', params.sort_by);
      if (params?.order !== undefined) qs.set('order', params.order);
      const q = qs.toString();
      return q ? `${apiBase()}/analytics/principals?${q}` : `${apiBase()}/analytics/principals`;
    },
  },

  agentTypes: {
    list: (): string => `${apiBase()}/agents/types`,
    create: (): string => `${apiBase()}/agents/types`,
    detail: (id: string): string =>
      `${apiBase()}/agents/types/${id}`,
    delete: (id: string): string =>
      `${apiBase()}/agents/types/${id}`,
    toolScopes: (): string => `${apiBase()}/agents/tool-scopes`,
    connectors: (): string => `${apiBase()}/agents/connectors`,
    mcpTools: (): string => `${apiBase()}/agents/mcp-tools`,
    effectiveAccess: (id: string): string =>
      `${apiBase()}/agents/types/${id}/effective-access`,
    /**
     * `/api/agents/types/effective-access`
     *
     * POST — resolves the effective-access view for a *draft* (unsaved)
     * connectivity config. Backs the Connectivity tab's live preview.
     */
    previewEffectiveAccess: (): string =>
      `${apiBase()}/agents/types/effective-access`,
    /**
     * `/api/agents/types/[typeId]/usage`
     *
     * GET — returns the agent type's 7-day token breakdown and spend, sourced
     *       from the inference gateway. Powers the token breakdown and spend
     *       cards on the agent overview page.
     */
    usage: (id: string): string =>
      `${apiBase()}/agents/types/${id}/usage`,
    /**
     * `/api/agents/types/usage`
     *
     * GET — returns the 7-day token breakdown and spend for every agent type in
     *       the tenant. Powers the total + per-agent cost on the agents list.
     */
    usageAll: (): string => `${apiBase()}/agents/types/usage`,
  },

  /**
   * `/api/agents/environments`
   *
   * list/create — the tenant's environments (egress-host + connector bundles).
   * detail/delete — full-replace (PUT) or delete a single environment by ID.
   */
  environments: {
    list: (): string => `${apiBase()}/agents/environments`,
    create: (): string => `${apiBase()}/agents/environments`,
    detail: (id: string): string =>
      `${apiBase()}/agents/environments/${id}`,
    delete: (id: string): string =>
      `${apiBase()}/agents/environments/${id}`,
  },

  /**
   * Triggers feature — webhook sources, triggers, and delivery log. Served by the
   * identity binary via the BFF.
   */
  triggerSources: {
    list: (): string => `${apiBase()}/trigger-sources`,
    create: (): string => `${apiBase()}/trigger-sources`,
    detail: (id: string): string =>
      `${apiBase()}/trigger-sources/${id}`,
    delete: (id: string): string =>
      `${apiBase()}/trigger-sources/${id}`,
  },

  triggers: {
    list: (agentTypeId?: string): string =>
      agentTypeId
        ? `${apiBase()}/triggers?agent_type_id=${encodeURIComponent(agentTypeId)}`
        : `${apiBase()}/triggers`,
    create: (): string => `${apiBase()}/triggers`,
    detail: (id: string): string => `${apiBase()}/triggers/${id}`,
    delete: (id: string): string => `${apiBase()}/triggers/${id}`,
  },

  triggerDeliveries: {
    list: (sourceId?: string): string =>
      sourceId
        ? `${apiBase()}/trigger-deliveries?source_id=${encodeURIComponent(sourceId)}`
        : `${apiBase()}/trigger-deliveries`,
  },

  /** Dev Environments feature — ephemeral, vcluster-backed Orbit installs. Served by the identity binary via the BFF. */
  devEnvironments: {
    list: (): string => `${apiBase()}/dev-environments`,
    create: (): string => `${apiBase()}/dev-environments`,
    detail: (id: string): string =>
      `${apiBase()}/dev-environments/${id}`,
    destroy: (id: string): string =>
      `${apiBase()}/dev-environments/${id}`,
    extend: (id: string): string =>
      `${apiBase()}/dev-environments/${id}/extend`,
    wake: (id: string): string =>
      `${apiBase()}/dev-environments/${id}/wake`,
    kubeconfig: (id: string): string =>
      `${apiBase()}/dev-environments/${id}/kubeconfig`,
  },

  /**
   * `/api/dev-environment/exit`
   *
   * GET — clears the host-side dev-environment routing cookie and redirects
   *       to `/`. Only meaningful when the dashboard is being browsed from
   *       inside a dev environment's own in-vcluster frontend.
   */
  devEnvironmentExit: {
    GET: (): string => `${apiBase()}/dev-environment/exit`,
  },

  runs: {
    list: (): string => `${apiBase()}/agents/runs`,
    listForType: (typeId: string): string =>
      `${apiBase()}/agents/types/${typeId}/runs`,
    start: (typeId: string): string =>
      `${apiBase()}/agents/types/${typeId}/runs`,
    detail: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}`,
    terminate: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}`,
    messages: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}/messages`,
    events: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}/events`,
    usage: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}/usage`,
    interrupt: (runId: string): string =>
      `${apiBase()}/agents/runs/${runId}/interrupt`,
  },

  /**
   * `/api/usage/budget`
   *
   * GET — returns the caller's current monthly budget status (spend vs limit).
   *       Returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached, has_limit }`.
   */
  usageBudget: {
    GET: (): string => `${apiBase()}/usage/budget`,
  },

  /**
   * `/api/catalog/pricing`
   *
   * GET — returns all model_pricing rows with synced, override, and effective
   *       price columns. Requires `platform:catalog:read` scope.
   */
  catalogPricing: {
    GET: (): string => `${apiBase()}/catalog/pricing`,
  },

  /**
   * `/api/catalog/pricing/[provider]/[modelId]/override`
   *
   * PUT    — set one or more pricing override columns for a provider/model pair.
   * DELETE — clear all pricing overrides (revert to synced prices).
   */
  catalogPricingOverride: {
    PUT: (
      provider: string,
      modelId: string,
    ): string =>
      `${apiBase()}/catalog/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}/override`,
    DELETE: (
      provider: string,
      modelId: string,
    ): string =>
      `${apiBase()}/catalog/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}/override`,
  },
} as const;

/**
 * The union of all BFF API route path strings producible by `ApiRoute`.
 *
 * Use this type when you need to accept or store an API route value outside
 * of a direct `ApiRoute.*.*()` call.
 */
export type ApiRoutePath =
  | ReturnType<(typeof ApiRoute)['health']['GET']>
  | ReturnType<(typeof ApiRoute)['identityHealth']['GET']>
  | ReturnType<(typeof ApiRoute)['identityUsers']['GET']>
  | ReturnType<(typeof ApiRoute)['logout']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['GET']>
  | ReturnType<(typeof ApiRoute)['discover']['PATH']>
  | ReturnType<(typeof ApiRoute)['models']['GET']>
  | ReturnType<(typeof ApiRoute)['policyStats']['GET']>
  | ReturnType<(typeof ApiRoute)['policyGrants']['GET']>
  | ReturnType<(typeof ApiRoute)['policyGrants']['POST']>
  | ReturnType<(typeof ApiRoute)['policyGrant']['DELETE']>
  | ReturnType<(typeof ApiRoute)['policyRoles']['GET']>
  | ReturnType<(typeof ApiRoute)['policyRoles']['PUT']>
  | ReturnType<(typeof ApiRoute)['policyRole']['DELETE']>
  | ReturnType<(typeof ApiRoute)['policyVocabulary']['GET']>
  | ReturnType<(typeof ApiRoute)['policyRoutes']['GET']>
  | ReturnType<(typeof ApiRoute)['policyExplain']['GET']>
  | ReturnType<(typeof ApiRoute)['policySimulate']['POST']>
  | ReturnType<(typeof ApiRoute)['usage']['GET']>
  | ReturnType<(typeof ApiRoute)['usageBudget']['GET']>
  | ReturnType<(typeof ApiRoute)['catalogPricing']['GET']>
  | ReturnType<(typeof ApiRoute)['catalogPricingOverride']['PUT']>
  | ReturnType<(typeof ApiRoute)['catalogPricingOverride']['DELETE']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['list']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['create']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['detail']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['delete']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['effectiveAccess']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['previewEffectiveAccess']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['usage']>
  | ReturnType<(typeof ApiRoute)['environments']['list']>
  | ReturnType<(typeof ApiRoute)['environments']['create']>
  | ReturnType<(typeof ApiRoute)['environments']['detail']>
  | ReturnType<(typeof ApiRoute)['environments']['delete']>
  | ReturnType<(typeof ApiRoute)['runs']['list']>
  | ReturnType<(typeof ApiRoute)['runs']['listForType']>
  | ReturnType<(typeof ApiRoute)['runs']['start']>
  | ReturnType<(typeof ApiRoute)['runs']['detail']>
  | ReturnType<(typeof ApiRoute)['runs']['terminate']>
  | ReturnType<(typeof ApiRoute)['runs']['messages']>
  | ReturnType<(typeof ApiRoute)['runs']['events']>
  | ReturnType<(typeof ApiRoute)['runs']['usage']>
  | ReturnType<(typeof ApiRoute)['runs']['interrupt']>
  | ReturnType<(typeof ApiRoute)['devEnvironmentExit']['GET']>
  | string; // analyticsMe, analytics, analyticsPrincipals include dynamic query strings
