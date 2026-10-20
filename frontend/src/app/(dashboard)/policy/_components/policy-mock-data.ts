import type {
  Grant,
  RoleEntry,
  RouteRegistration,
  ScopeEntryWithUsage,
  ScopesExplainResponse,
  StatsResponse,
} from '../_types';
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';

/** Scopes the policy pages need beyond the sidebar set, so write controls render. */
export const POLICY_WRITE_SCOPES = [
  'platform:grants:read',
  'platform:grants:write',
  'platform:roles:read',
  'platform:roles:write',
  'platform:routes:read',
  'platform:vocabulary:read',
  'triggers:read',
];

export const TENANT_ID = 'ten_8f3a21c0';

const minutesAgo = (m: number): string => new Date(Date.now() - m * 60_000).toISOString();
const daysAgo = (d: number): string => minutesAgo(d * 24 * 60);

// ── Vocabulary ───────────────────────────────────────────────────────────────

type ScopeSeed = [scope: string, description: string, grants: number, roles: number];

const SCOPE_SEEDS: ScopeSeed[] = [
  ['agents:read', 'List and inspect agents and their runs', 6, 3],
  ['agents:write', 'Create, update and delete agents', 2, 2],
  ['agents:run:start', 'Start a run of an agent', 4, 2],
  ['agents:run:terminate', 'Terminate a running agent', 1, 1],
  ['inference:chat:create', 'Call chat completions through the gateway', 7, 3],
  ['inference:models:read', 'List the models available to the tenant', 5, 3],
  ['mcp:github:read', 'Read repositories, issues and pull requests', 4, 2],
  ['mcp:github:write', 'Comment on pull requests and open issues', 2, 1],
  ['mcp:slack:read', 'Read channels and messages', 1, 1],
  ['mcp:slack:write', 'Post messages to channels', 1, 0],
  ['mcp:linear:read', 'Read Linear issues and projects', 0, 0],
  ['platform:grants:read', 'List grants for the tenant', 1, 2],
  ['platform:grants:write', 'Create and delete grants', 0, 1],
  ['platform:roles:read', 'List role mappings', 1, 2],
  ['platform:roles:write', 'Create and edit role mappings', 0, 1],
  ['platform:routes:read', 'List route to scope registrations', 1, 2],
  ['platform:vocabulary:read', 'List the scope vocabulary', 1, 2],
  ['triggers:read', 'List triggers and deliveries', 2, 2],
  ['triggers:write', 'Create and update triggers', 0, 1],
  ['tokens:read', 'List personal access tokens', 0, 1],
];

export const VOCABULARY: ScopeEntryWithUsage[] = SCOPE_SEEDS.map(
  ([scope, description, grant_count, role_count], i) => {
    const parts = scope.split(':');
    return {
      scope,
      resource_type: parts[0] ?? scope,
      path: parts.slice(1, -1),
      action: parts[parts.length - 1] ?? '',
      description,
      created_at: daysAgo(40 - i),
      grant_count,
      role_count,
    };
  },
);

export const AVAILABLE_SCOPES: string[] = VOCABULARY.map((s) => s.scope);

// ── Grants ───────────────────────────────────────────────────────────────────

export const GRANTS: Grant[] = [
  {
    id: 'grt_01j8x2k4m7',
    tenant_id: TENANT_ID,
    principal_type: 'AGENT',
    principal_id: 'a3f1c9e2-5b7d-4c1a-9e60-2d8b4f7a1c35',
    scopes: ['agents:read', 'mcp:github:read', 'mcp:github:write', 'inference:chat:create'],
    budget: { limit_usd: 50, reset_cadence: 'monthly', per_run_limit_usd: 5 },
    created_at: minutesAgo(35),
  },
  {
    id: 'grt_01j8x1q9d3',
    tenant_id: TENANT_ID,
    principal_type: 'AGENT',
    principal_id: '7c2d8e41-0a9f-4b36-b1d5-6e3f9a2c8d17',
    scopes: ['agents:read', 'agents:run:start', 'mcp:github:read', 'inference:chat:create'],
    created_at: minutesAgo(180),
  },
  {
    id: 'grt_01j8w9r2t6',
    tenant_id: TENANT_ID,
    principal_type: 'HUMAN',
    principal_id: 'usr_42b7e9',
    scopes: ['agents:read', 'agents:write', 'platform:grants:read', 'platform:roles:read'],
    created_at: daysAgo(2),
  },
  {
    id: 'grt_01j8w4n8c1',
    tenant_id: TENANT_ID,
    principal_type: 'HUMAN',
    principal_id: 'usr_9d51a3',
    scopes: ['agents:read', 'inference:models:read'],
    created_at: daysAgo(4),
  },
  {
    id: 'grt_01j8v7h5p0',
    tenant_id: TENANT_ID,
    principal_type: 'PAT',
    principal_id: 'pat_5e0b72c4',
    scopes: ['inference:chat:create', 'inference:models:read'],
    budget: { limit_usd: 20, reset_cadence: 'weekly' },
    created_at: daysAgo(6),
  },
  {
    id: 'grt_01j8v2b6y9',
    tenant_id: TENANT_ID,
    principal_type: 'SERVICE',
    principal_id: 'svc-triggers',
    scopes: ['agents:run:start', 'triggers:read'],
    created_at: daysAgo(12),
  },
  {
    id: 'grt_01j8u5e1w4',
    tenant_id: TENANT_ID,
    principal_type: 'AGENT',
    principal_id: 'e94b6a07-3d12-4f8c-a5b9-1c7e0d3f2a68',
    scopes: ['agents:read', 'mcp:slack:read', 'mcp:slack:write'],
    created_at: daysAgo(15),
  },
];

/** Enough grants to fill a 50 row page of the paginated list. */
export const MANY_GRANTS: Grant[] = Array.from({ length: 24 }, (_, i) => {
  const base = GRANTS[i % GRANTS.length]!;
  return {
    ...base,
    id: `grt_01j8${String(i).padStart(2, '0')}z${i}m3`,
    principal_id:
      base.principal_type === 'AGENT'
        ? `${(0xa0000000 + i * 7919).toString(16)}-5b7d-4c1a-9e60-2d8b4f7a1c35`
        : `${base.principal_id}${i}`,
    created_at: daysAgo(i + 1),
  };
});

export const PAT_ATTRIBUTION: Record<string, PATGrantAttribution> = {
  pat_5e0b72c4: {
    pat_name: 'ci-pipeline',
    owner: { id: 'usr_9d51a3', email: 'maya.chen@kora.example', display_name: 'Maya Chen' },
  },
};

export const USER_ATTRIBUTION: Record<string, PATAttributionEntry> = {
  usr_42b7e9: { id: 'usr_42b7e9', email: 'dev@kora.example', display_name: 'Dev Operator' },
  usr_9d51a3: { id: 'usr_9d51a3', email: 'maya.chen@kora.example', display_name: 'Maya Chen' },
};

// ── Roles ────────────────────────────────────────────────────────────────────

export const ROLES: RoleEntry[] = [
  {
    id: 'rol_01j7a1',
    tenant_id: TENANT_ID,
    role: 'platform-admin',
    scopes: AVAILABLE_SCOPES,
    created_at: daysAgo(40),
    reserved: true,
  },
  {
    id: 'rol_01j7a2',
    tenant_id: TENANT_ID,
    role: 'ml-engineer',
    scopes: [
      'agents:read',
      'agents:write',
      'agents:run:start',
      'inference:chat:create',
      'inference:models:read',
      'mcp:github:read',
    ],
    budget: { limit_usd: 200, reset_cadence: 'monthly', pooled: false },
    created_at: daysAgo(21),
  },
  {
    id: 'rol_01j7a3',
    tenant_id: TENANT_ID,
    role: 'security-reviewer',
    scopes: ['agents:read', 'mcp:github:read', 'platform:grants:read', 'platform:routes:read'],
    created_at: daysAgo(14),
  },
  {
    id: 'rol_01j7a4',
    tenant_id: TENANT_ID,
    role: 'support',
    scopes: ['agents:read', 'triggers:read'],
    budget: { limit_usd: 500, reset_cadence: 'monthly', pooled: true },
    created_at: daysAgo(9),
  },
];

// ── Routes ───────────────────────────────────────────────────────────────────

export const ROUTE_REGISTRATIONS: RouteRegistration[] = [
  {
    service: 'identity',
    tenant_id: '',
    registered_at: minutesAgo(600),
    routes: [
      { path_prefix: '/v1/tokens', method: 'GET', required_scope: 'tokens:read' },
      { path_prefix: '/v1/identity/users', method: 'GET', required_scope: 'platform:grants:read' },
    ],
  },
  {
    service: 'orbit-agents',
    tenant_id: '',
    registered_at: minutesAgo(600),
    routes: [
      { path_prefix: '/v1/agents', method: 'GET', required_scope: 'agents:read' },
      { path_prefix: '/v1/agents', method: 'POST', required_scope: 'agents:write' },
      { path_prefix: '/v1/agents', method: 'PUT', required_scope: 'agents:write' },
      { path_prefix: '/v1/agents', method: 'DELETE', required_scope: 'agents:write' },
      { path_prefix: '/v1/runs', method: 'POST', required_scope: 'agents:run:start' },
      { path_prefix: '/v1/runs', method: 'GET', required_scope: 'agents:read' },
    ],
  },
  {
    service: 'inference-gateway',
    tenant_id: '',
    registered_at: minutesAgo(600),
    routes: [
      {
        path_prefix: '/v1/chat/completions',
        method: 'POST',
        required_scope: 'inference:chat:create',
      },
      { path_prefix: '/v1/models', method: 'GET', required_scope: 'inference:models:read' },
    ],
  },
  {
    service: 'policy-service',
    tenant_id: '',
    registered_at: minutesAgo(600),
    routes: [
      { path_prefix: '/v1/policy/grants', method: 'GET', required_scope: 'platform:grants:read' },
      { path_prefix: '/v1/policy/grants', method: 'POST', required_scope: 'platform:grants:write' },
      { path_prefix: '/v1/policy/roles', method: 'GET', required_scope: 'platform:roles:read' },
      { path_prefix: '/v1/policy/roles', method: 'PUT', required_scope: 'platform:roles:write' },
      { path_prefix: '/v1/policy/routes', method: 'GET', required_scope: 'platform:routes:read' },
      {
        path_prefix: '/v1/policy/vocabulary',
        method: 'GET',
        required_scope: 'platform:vocabulary:read',
      },
    ],
  },
  {
    service: 'orbit-triggers',
    tenant_id: '',
    registered_at: minutesAgo(600),
    routes: [
      { path_prefix: '/v1/triggers', method: 'GET', required_scope: 'triggers:read' },
      { path_prefix: '/v1/triggers', method: 'POST', required_scope: 'triggers:write' },
      { path_prefix: '/v1/triggers', method: 'PATCH', required_scope: 'triggers:write' },
    ],
  },
];

// ── Overview stats ───────────────────────────────────────────────────────────

export const STATS: StatsResponse = {
  grants: {
    total: GRANTS.length,
    by_principal_type: { AGENT: 3, HUMAN: 2, PAT: 1, SERVICE: 1 },
  },
  roles: { total: ROLES.length, reserved: 1, custom: ROLES.length - 1 },
  vocabulary: {
    total: VOCABULARY.length,
    by_resource_type: { agents: 4, inference: 2, mcp: 5, platform: 6, triggers: 2, tokens: 1 },
  },
  routes: { total: ROUTE_REGISTRATIONS.reduce((n, r) => n + r.routes.length, 0) },
  bundles: {
    tenants: [{ tenant_id: TENANT_ID, last_compiled_at: minutesAgo(12), status: 'current' }],
  },
};

export const STATS_STALE: StatsResponse = {
  ...STATS,
  bundles: {
    tenants: [
      { tenant_id: TENANT_ID, last_compiled_at: minutesAgo(190), status: 'stale' },
      { tenant_id: 'ten_c41d9e77', last_compiled_at: minutesAgo(14), status: 'current' },
    ],
  },
};

export const STATS_EMPTY: StatsResponse = {
  grants: { total: 0, by_principal_type: { AGENT: 0, HUMAN: 0, PAT: 0, SERVICE: 0 } },
  roles: { total: 0, reserved: 0, custom: 0 },
  vocabulary: { total: 0, by_resource_type: {} },
  routes: { total: 0 },
  bundles: { tenants: [] },
};

/** Most recent five grants, matching how the overview page slices them. */
export const RECENT_GRANTS: Grant[] = [...GRANTS]
  .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  .slice(0, 5);

// ── Evaluate ─────────────────────────────────────────────────────────────────

export const EXPLAIN: ScopesExplainResponse = {
  principal_type: 'HUMAN',
  principal_id: 'dev@kora.example',
  tenant_id: TENANT_ID,
  from_roles: [
    {
      role: 'ml-engineer',
      scopes: ['agents:read', 'agents:write', 'agents:run:start', 'inference:chat:create'],
    },
    {
      role: 'security-reviewer',
      scopes: ['agents:read', 'mcp:github:read', 'platform:grants:read'],
    },
  ],
  from_grants: [{ grant_id: 'grt_01j8w9r2t6', scopes: ['agents:read', 'platform:roles:read'] }],
  effective_scopes: [
    'agents:read',
    'agents:write',
    'agents:run:start',
    'inference:chat:create',
    'mcp:github:read',
    'platform:grants:read',
    'platform:roles:read',
  ],
  evaluated_at: minutesAgo(0),
};

export const EXPLAIN_NO_SCOPES: ScopesExplainResponse = {
  principal_type: 'AGENT',
  principal_id: 'b02f5d19-8e47-4a63-9c10-7d4e2a6b3f81',
  tenant_id: TENANT_ID,
  from_roles: [],
  from_grants: [],
  effective_scopes: [],
  evaluated_at: minutesAgo(0),
};

export const AVAILABLE_ROLES: string[] = ROLES.map((r) => r.role);
