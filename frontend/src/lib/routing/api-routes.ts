/**
 * api-routes.ts
 *
 * Single source of truth for every BFF (Backend-for-Frontend) API route in
 * the kora app.
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

import type { AgentInsightsPeriod } from '@orbit/shared/api';

// ── Base segments ─────────────────────────────────────────────────────────────

const AUTH_BASE = '/api/auth' as const;
const API_BASE = '/api' as const;

// ── Route registry ────────────────────────────────────────────────────────────

export const ApiRoute = {
  /**
   * `/api/health`
   *
   * Reachable without a session — used by monitoring and load-balancer health
   * checks that arrive through the Istio gateway.
   */
  health: {
    GET: (): `${typeof API_BASE}/health` => `${API_BASE}/health`,
  },

  /**
   * `/api/identity/health`
   *
   * Proxies the identity service health check server-side so the browser never
   * communicates with the Go API directly. Displayed by the HealthStatus
   * component on the dashboard.
   */
  identityHealth: {
    GET: (): `${typeof API_BASE}/identity/health` => `${API_BASE}/identity/health`,
  },

  /**
   * `/api/identity/users`
   *
   * Returns all users in the caller's tenant. Does not require the
   * analytics scope — safe to call from admin pages.
   */
  identityUsers: {
    GET: (): `${typeof API_BASE}/identity/users` => `${API_BASE}/identity/users`,
  },

  /**
   * `/api/auth/logout`
   *
   * Revokes the kora tokens server-side (best-effort) and destroys the
   * iron-session cookie, then redirects to `/login`.
   */
  logout: {
    POST: (): `${typeof AUTH_BASE}/logout` => `${AUTH_BASE}/logout`,
  },

  /**
   * `/api/auth/session`
   *
   * POST — called by the OIDC callback page with `{ id_token }` to exchange
   *        the provider id_token for a kora session cookie.
   *
   * GET  — returns the public (non-sensitive) portion of the current session
   *        for client components that need to know who is logged in.
   */
  session: {
    POST: (): `${typeof AUTH_BASE}/session` => `${AUTH_BASE}/session`,
    GET: (): `${typeof AUTH_BASE}/session` => `${AUTH_BASE}/session`,
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
    PATH: (): `${typeof AUTH_BASE}/discover` => `${AUTH_BASE}/discover`,
    GET: (email: string): `${typeof AUTH_BASE}/discover?email=${string}` =>
      `${AUTH_BASE}/discover?email=${encodeURIComponent(email)}`,
  },

  /**
   * `/api/models`
   *
   * GET — proxies to the Go inference gateway `GET /v1/models` and returns
   *       the full list of available models. Requires an authenticated session.
   */
  models: {
    GET: (): `${typeof API_BASE}/models` => `${API_BASE}/models`,
  },

  /**
   * `/api/policy/stats`
   *
   * GET — proxies to the policy-service `GET /api/v1/policy/stats`.
   */
  policyStats: {
    GET: (): `${typeof API_BASE}/policy/stats` => `${API_BASE}/policy/stats`,
  },

  /**
   * `/api/policy/grants`
   *
   * GET  — list grants for the caller's tenant.
   * POST — create a new grant.
   */
  policyGrants: {
    GET: (): `${typeof API_BASE}/policy/grants` => `${API_BASE}/policy/grants`,
    POST: (): `${typeof API_BASE}/policy/grants` => `${API_BASE}/policy/grants`,
  },

  /**
   * `/api/policy/grants/[id]`
   *
   * DELETE — revoke a grant by ID.
   */
  policyGrant: {
    DELETE: (id: string): `${typeof API_BASE}/policy/grants/${string}` =>
      `${API_BASE}/policy/grants/${id}`,
  },

  /**
   * `/api/policy/roles`
   *
   * GET — list role→scope mappings for the caller's tenant.
   * PUT — create or replace a role→scope mapping.
   */
  policyRoles: {
    GET: (): `${typeof API_BASE}/policy/roles` => `${API_BASE}/policy/roles`,
    PUT: (): `${typeof API_BASE}/policy/roles` => `${API_BASE}/policy/roles`,
  },

  /**
   * `/api/policy/roles/[role]`
   *
   * DELETE — remove a role→scope mapping.
   */
  policyRole: {
    DELETE: (role: string): `${typeof API_BASE}/policy/roles/${string}` =>
      `${API_BASE}/policy/roles/${encodeURIComponent(role)}`,
  },

  /**
   * `/api/policy/vocabulary`
   *
   * GET — list scope vocabulary with optional usage counts.
   */
  policyVocabulary: {
    GET: (): `${typeof API_BASE}/policy/vocabulary` => `${API_BASE}/policy/vocabulary`,
  },

  /**
   * `/api/policy/routes`
   *
   * GET — list all registered route→scope mappings.
   */
  policyRoutes: {
    GET: (): `${typeof API_BASE}/policy/routes` => `${API_BASE}/policy/routes`,
  },

  /**
   * `/api/policy/evaluate/explain`
   *
   * GET — scope explain for a principal (proxies to policy-gateway).
   */
  policyExplain: {
    GET: (): `${typeof API_BASE}/policy/evaluate/explain` => `${API_BASE}/policy/evaluate/explain`,
  },

  /**
   * `/api/policy/evaluate/simulate`
   *
   * POST — simulate a policy check (proxies to policy-gateway).
   */
  policySimulate: {
    POST: (): `${typeof API_BASE}/policy/evaluate/simulate` =>
      `${API_BASE}/policy/evaluate/simulate`,
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
      return q ? `${API_BASE}/usage?${q}` : `${API_BASE}/usage`;
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
      return q ? `${API_BASE}/analytics/me?${q}` : `${API_BASE}/analytics/me`;
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
      return q ? `${API_BASE}/analytics?${q}` : `${API_BASE}/analytics`;
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
      return q ? `${API_BASE}/analytics/principals?${q}` : `${API_BASE}/analytics/principals`;
    },
  },

  /**
   * `/api/analytics/principals/{id}`
   *
   * GET — proxies to the Go inference gateway `GET /v1/analytics/principals/{id}`
   *       and returns the full usage breakdown (time-series, KPIs, cost-by-model —
   *       same shape as `/api/analytics/me`) for the specified principal. Backs the
   *       admin drill-down from a Principals table row. Requires `platform:analytics:read`
   *       scope. Accepts optional `days` (7 | 30 | 90) and `granularity` params.
   */
  analyticsPrincipalById: {
    GET: (id: string, params?: { days?: number; granularity?: string }): string => {
      const qs = new URLSearchParams();
      if (params?.days !== undefined) qs.set('days', String(params.days));
      if (params?.granularity) qs.set('granularity', params.granularity);
      const q = qs.toString();
      const path = `${API_BASE}/analytics/principals/${encodeURIComponent(id)}`;
      return q ? `${path}?${q}` : path;
    },
  },

  /**
   * `/api/agents/insights?period=24h|7d|30d|90d`
   *
   * GET: the Agents page rollups. Usage, model split, spend over time, budgets
   * and run outcomes for one window.
   */
  agentInsights: {
    window: (
      period: AgentInsightsPeriod,
    ): `${typeof API_BASE}/agents/insights?period=${AgentInsightsPeriod}` =>
      `${API_BASE}/agents/insights?period=${period}`,
  },

  agentTypes: {
    list: (): `${typeof API_BASE}/agents/types` => `${API_BASE}/agents/types`,
    create: (): `${typeof API_BASE}/agents/types` => `${API_BASE}/agents/types`,
    detail: (id: string): `${typeof API_BASE}/agents/types/${string}` =>
      `${API_BASE}/agents/types/${id}`,
    delete: (id: string): `${typeof API_BASE}/agents/types/${string}` =>
      `${API_BASE}/agents/types/${id}`,
    connectors: (): `${typeof API_BASE}/agents/connectors` => `${API_BASE}/agents/connectors`,
    mcpTools: (): `${typeof API_BASE}/agents/mcp-tools` => `${API_BASE}/agents/mcp-tools`,
    effectiveAccess: (id: string): `${typeof API_BASE}/agents/types/${string}/effective-access` =>
      `${API_BASE}/agents/types/${id}/effective-access`,
    /**
     * `/api/agents/types/effective-access`
     *
     * POST — resolves the effective-access view for a *draft* (unsaved)
     * connectivity config. Backs the Connectivity tab's live preview.
     */
    previewEffectiveAccess: (): `${typeof API_BASE}/agents/types/effective-access` =>
      `${API_BASE}/agents/types/effective-access`,
    /**
     * `/api/agents/types/overview/[slug]`
     *
     * GET: one agent's usage, spend, budget and run stats for one window. Powers
     *      the agent Overview.
     */
    overview: (
      slug: string,
      period: AgentInsightsPeriod,
    ): `${typeof API_BASE}/agents/types/overview/${string}?period=${AgentInsightsPeriod}` =>
      `${API_BASE}/agents/types/overview/${encodeURIComponent(slug)}?period=${period}`,
    /**
     * `/api/agents/types/drafts`
     *
     * Agent type drafts — the builder's persistence. A draft is a saved form,
     * private to the caller; it holds no grant and provisions nothing
     * (docs/adr/2026-07-17-agent-builder.md §2).
     */
    drafts: (): `${typeof API_BASE}/agents/types/drafts` => `${API_BASE}/agents/types/drafts`,
    draft: (id: string): `${typeof API_BASE}/agents/types/drafts/${string}` =>
      `${API_BASE}/agents/types/drafts/${id}`,
  },

  /**
   * `/api/agents/environments`
   *
   * list/create — the tenant's environments (egress-host + connector bundles).
   * detail/delete — full-replace (PUT) or delete a single environment by ID.
   */
  environments: {
    list: (): `${typeof API_BASE}/agents/environments` => `${API_BASE}/agents/environments`,
    create: (): `${typeof API_BASE}/agents/environments` => `${API_BASE}/agents/environments`,
    detail: (id: string): `${typeof API_BASE}/agents/environments/${string}` =>
      `${API_BASE}/agents/environments/${id}`,
    delete: (id: string): `${typeof API_BASE}/agents/environments/${string}` =>
      `${API_BASE}/agents/environments/${id}`,
  },

  /**
   * Triggers feature — webhook sources, triggers, and delivery log. Served by the
   * identity binary via the BFF.
   */
  triggerSources: {
    list: (): `${typeof API_BASE}/trigger-sources` => `${API_BASE}/trigger-sources`,
    create: (): `${typeof API_BASE}/trigger-sources` => `${API_BASE}/trigger-sources`,
    detail: (id: string): `${typeof API_BASE}/trigger-sources/${string}` =>
      `${API_BASE}/trigger-sources/${id}`,
    delete: (id: string): `${typeof API_BASE}/trigger-sources/${string}` =>
      `${API_BASE}/trigger-sources/${id}`,
    eventTypes: (id: string): `${typeof API_BASE}/trigger-sources/${string}/event-types` =>
      `${API_BASE}/trigger-sources/${id}/event-types`,
  },

  /** Per-provider schema catalogue: event types, fields and sender associations. */
  triggerProviders: {
    eventTypes: (provider: string): `${typeof API_BASE}/trigger-providers/${string}/event-types` =>
      `${API_BASE}/trigger-providers/${encodeURIComponent(provider)}/event-types`,
    fields: (
      provider: string,
      eventType: string,
    ): `${typeof API_BASE}/trigger-providers/${string}/fields?event_type=${string}` =>
      `${API_BASE}/trigger-providers/${encodeURIComponent(provider)}/fields?event_type=${encodeURIComponent(eventType)}`,
  },
  triggers: {
    list: (agentTypeId?: string): string =>
      agentTypeId
        ? `${API_BASE}/triggers?agent_type_id=${encodeURIComponent(agentTypeId)}`
        : `${API_BASE}/triggers`,
    create: (): `${typeof API_BASE}/triggers` => `${API_BASE}/triggers`,
    detail: (id: string): `${typeof API_BASE}/triggers/${string}` => `${API_BASE}/triggers/${id}`,
    delete: (id: string): `${typeof API_BASE}/triggers/${string}` => `${API_BASE}/triggers/${id}`,
    fire: (id: string): `${typeof API_BASE}/triggers/${string}/fire` =>
      `${API_BASE}/triggers/${id}/fire`,
    /** Next fires for a cron expression, resolved by the scheduler itself. */
    schedulePreview: (): `${typeof API_BASE}/triggers/schedule-preview` =>
      `${API_BASE}/triggers/schedule-preview`,
  },

  triggerDeliveries: {
    list: (sourceId?: string): string =>
      sourceId
        ? `${API_BASE}/trigger-deliveries?source_id=${encodeURIComponent(sourceId)}`
        : `${API_BASE}/trigger-deliveries`,
    /** Replay a delivery. Answers with the NEW delivery's id. */
    redeliver: (id: string): `${typeof API_BASE}/trigger-deliveries/${string}/redeliver` =>
      `${API_BASE}/trigger-deliveries/${id}/redeliver`,
  },

  runs: {
    list: (): `${typeof API_BASE}/agents/runs` => `${API_BASE}/agents/runs`,
    /** Run stats across every agent the caller may read, for one window. */
    stats: (
      period: AgentInsightsPeriod,
    ): `${typeof API_BASE}/agents/runs/stats?period=${AgentInsightsPeriod}` =>
      `${API_BASE}/agents/runs/stats?period=${period}`,
    listForType: (typeId: string): `${typeof API_BASE}/agents/types/${string}/runs` =>
      `${API_BASE}/agents/types/${typeId}/runs`,
    start: (typeId: string): `${typeof API_BASE}/agents/types/${string}/runs` =>
      `${API_BASE}/agents/types/${typeId}/runs`,
    detail: (runId: string): `${typeof API_BASE}/agents/runs/${string}` =>
      `${API_BASE}/agents/runs/${runId}`,
    terminate: (runId: string): `${typeof API_BASE}/agents/runs/${string}` =>
      `${API_BASE}/agents/runs/${runId}`,
    messages: (runId: string): `${typeof API_BASE}/agents/runs/${string}/messages` =>
      `${API_BASE}/agents/runs/${runId}/messages`,
    events: (runId: string): `${typeof API_BASE}/agents/runs/${string}/events` =>
      `${API_BASE}/agents/runs/${runId}/events`,
  },

  /**
   * `/api/usage/budget`
   *
   * GET — returns the caller's current monthly budget status (spend vs limit).
   *       Returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached, has_limit }`.
   */
  usageBudget: {
    GET: (): `${typeof API_BASE}/usage/budget` => `${API_BASE}/usage/budget`,
  },

  /**
   * `/api/catalog/pricing`
   *
   * GET — returns all model_pricing rows with synced, override, and effective
   *       price columns. Requires `platform:catalog:read` scope.
   */
  catalogPricing: {
    GET: (): `${typeof API_BASE}/catalog/pricing` => `${API_BASE}/catalog/pricing`,
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
    ): `${typeof API_BASE}/catalog/pricing/${string}/${string}/override` =>
      `${API_BASE}/catalog/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}/override`,
    DELETE: (
      provider: string,
      modelId: string,
    ): `${typeof API_BASE}/catalog/pricing/${string}/${string}/override` =>
      `${API_BASE}/catalog/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}/override`,
  },

  /**
   * `/api/marketplace/skills`
   *
   * GET — returns one result page of Marketplace Skills and the Marketplace
   *       list. Accepts `marketplace`, `query`, `page`, `pageSize`, and
   *       `sort`. The BFF validates each value and uses the default for an
   *       invalid value.
   */
  marketplaceSkills: {
    GET: (params: {
      marketplace: string;
      query: string;
      page: number;
      pageSize: number;
      sort: string;
    }): `${typeof API_BASE}/marketplace/skills?${string}` => {
      const qs = new URLSearchParams();
      if (params.marketplace) qs.set('marketplace', params.marketplace);
      if (params.query) qs.set('query', params.query);
      qs.set('page', String(params.page));
      qs.set('pageSize', String(params.pageSize));
      qs.set('sort', params.sort);
      return `${API_BASE}/marketplace/skills?${qs.toString()}`;
    },
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
  | `${typeof AUTH_BASE}/discover?email=${string}`
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
  | ReturnType<(typeof ApiRoute)['marketplaceSkills']['GET']>
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
  | ReturnType<(typeof ApiRoute)['agentTypes']['overview']>
  | ReturnType<(typeof ApiRoute)['environments']['list']>
  | ReturnType<(typeof ApiRoute)['environments']['create']>
  | ReturnType<(typeof ApiRoute)['environments']['detail']>
  | ReturnType<(typeof ApiRoute)['environments']['delete']>
  | ReturnType<(typeof ApiRoute)['runs']['list']>
  | ReturnType<(typeof ApiRoute)['runs']['listForType']>
  | ReturnType<(typeof ApiRoute)['runs']['stats']>
  | ReturnType<(typeof ApiRoute)['runs']['start']>
  | ReturnType<(typeof ApiRoute)['runs']['detail']>
  | ReturnType<(typeof ApiRoute)['runs']['terminate']>
  | ReturnType<(typeof ApiRoute)['runs']['messages']>
  | ReturnType<(typeof ApiRoute)['runs']['events']>
  | string; // analyticsMe, analytics, analyticsPrincipals, analyticsPrincipalById include dynamic query strings
