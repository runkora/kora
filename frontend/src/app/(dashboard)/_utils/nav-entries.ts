import type { IconName } from '@orbit/terra';

import { APP_ROUTES, AppRoute } from '@orbit/lib/routing';

// ─── Types ────────────────────────────────────────────────────────────────────

export type NavEntry = {
  label: string;
  href: AppRoute;
  icon: IconName;
  badge?: number;
  wip?: boolean;
  requiredScope?: string;
};

export type NavGroup = { label: string; items: NavEntry[] };

// ─── Nav arrays ───────────────────────────────────────────────────────────────

const MAIN_NAV: NavEntry[] = [
  { label: 'Dashboard', href: APP_ROUTES.home, icon: 'layout-dashboard' },
  { label: 'Agents', href: APP_ROUTES.agents.list, icon: 'bot' },
  { label: 'Event Spine', href: APP_ROUTES.events, icon: 'activity', wip: true },
  { label: 'Credentials', href: APP_ROUTES.credentials, icon: 'key', wip: true },
];

const SECURITY_NAV: NavEntry[] = [
  { label: 'Identity', href: APP_ROUTES.identity, icon: 'user', wip: true },
];

const POLICY_NAV: NavEntry[] = [
  {
    label: 'Overview',
    href: APP_ROUTES.policy.overview,
    icon: 'bar-chart',
    requiredScope: 'platform:grants:read',
  },
  {
    label: 'Grants',
    href: APP_ROUTES.policy.grants,
    icon: 'shield-check',
    requiredScope: 'platform:grants:read',
  },
  {
    label: 'Roles',
    href: APP_ROUTES.policy.roles,
    icon: 'user-check',
    requiredScope: 'platform:roles:read',
  },
  {
    label: 'Vocabulary',
    href: APP_ROUTES.policy.vocabulary,
    icon: 'library',
    requiredScope: 'platform:grants:read',
  },
  {
    label: 'Routes',
    href: APP_ROUTES.policy.routes,
    icon: 'route',
    requiredScope: 'platform:grants:read',
  },
  {
    label: 'Evaluate',
    href: APP_ROUTES.policy.evaluate,
    icon: 'flask',
    requiredScope: 'platform:grants:read',
  },
];

const SYSTEM_NAV: NavEntry[] = [
  { label: 'Storage', href: APP_ROUTES.storage, icon: 'database', wip: true },
  { label: 'Settings', href: APP_ROUTES.settings, icon: 'settings', wip: true },
];

const MODEL_GATEWAY_NAV: NavEntry[] = [
  {
    label: 'Overview',
    href: APP_ROUTES.modelGateway.overview,
    icon: 'bar-chart',
    requiredScope: 'platform:analytics:read',
  },
  { label: 'Models', href: APP_ROUTES.modelGateway.models, icon: 'sparkles' },
  {
    label: 'Catalog',
    href: APP_ROUTES.modelGateway.catalog,
    icon: 'layers',
    requiredScope: 'platform:catalog:write',
  },
  { label: 'My Usage', href: APP_ROUTES.modelGateway.myUsage, icon: 'line-chart' },
  {
    label: 'Analytics',
    href: APP_ROUTES.modelGateway.analytics,
    icon: 'bar-chart',
    requiredScope: 'platform:analytics:read',
  },
  {
    label: 'Budgets',
    href: APP_ROUTES.modelGateway.budgets,
    icon: 'dollar-sign',
    requiredScope: 'platform:budgets:read',
  },
  { label: 'Logs', href: APP_ROUTES.modelGateway.logs, icon: 'activity' },
  { label: 'API Keys', href: APP_ROUTES.modelGateway.keys, icon: 'key' },
  { label: 'Setup', href: APP_ROUTES.modelGateway.setup, icon: 'wrench' },
];

// ─── Groups ───────────────────────────────────────────────────────────────────

export const NAV_GROUPS: NavGroup[] = [
  { label: 'Control Plane', items: MAIN_NAV },
  { label: 'Security', items: SECURITY_NAV },
  { label: 'Policy', items: POLICY_NAV },
  { label: 'Model Gateway', items: MODEL_GATEWAY_NAV },
  { label: 'System', items: SYSTEM_NAV },
];
