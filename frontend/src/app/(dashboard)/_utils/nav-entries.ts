import {
  Activity,
  BarChart3,
  Bot,
  Database,
  Key,
  KeyRound,
  LayoutDashboard,
  LineChart,
  Settings,
  ShieldCheck,
  Sparkles,
  User,
  Wrench,
} from 'lucide-react';

import { APP_ROUTES, AppRoute } from '@orbit/lib/routing';

// ─── Types ────────────────────────────────────────────────────────────────────

export type NavEntry = {
  /**
   * Display text and designated React list key.
   *
   * Labels are stable slugs in practice (copy changes go through a deliberate
   * PR), so coupling the key to `label` is acceptable. If labels ever need to
   * change independently of identity — e.g. for i18n, A/B copy tests, or
   * programmatically-generated nav — introduce a separate opaque `id: string`
   * field and switch the map key over to that.
   */
  label: string;
  href: AppRoute;
  icon: React.ComponentType<{ className?: string }>;
  badge?: number;
  /** True when no page exists yet — renders as disabled (non-navigable). */
  wip?: boolean;
  /**
   * When set, the entry is hidden from the sidebar unless the principal's
   * scope set includes this scope. Entries without this field are always visible.
   * See ADR-0037 for the permission projection model.
   */
  requiredScope?: string;
};

export type NavGroup = { label: string; items: NavEntry[] };

// ─── Nav arrays ───────────────────────────────────────────────────────────────

const MAIN_NAV: NavEntry[] = [
  { label: 'Dashboard', href: APP_ROUTES.home, icon: LayoutDashboard },
  { label: 'Agents', href: APP_ROUTES.agents, icon: Bot, badge: 12, wip: true },
  { label: 'Event Spine', href: APP_ROUTES.events, icon: Activity, wip: true },
  { label: 'Credentials', href: APP_ROUTES.credentials, icon: KeyRound, wip: true },
];

const SECURITY_NAV: NavEntry[] = [
  {
    label: 'Policy Engine',
    href: APP_ROUTES.policy,
    icon: ShieldCheck,
    wip: true,
    requiredScope: 'platform:grants:read',
  },
  { label: 'Identity', href: APP_ROUTES.identity, icon: User, wip: true },
];

const SYSTEM_NAV: NavEntry[] = [
  { label: 'Storage', href: APP_ROUTES.storage, icon: Database, wip: true },
  { label: 'Settings', href: APP_ROUTES.settings, icon: Settings, wip: true },
];

const MODEL_GATEWAY_NAV: NavEntry[] = [
  { label: 'Overview', href: APP_ROUTES.modelGateway.overview, icon: BarChart3 },
  { label: 'Models', href: APP_ROUTES.modelGateway.models, icon: Sparkles },
  { label: 'Usage', href: APP_ROUTES.modelGateway.usage, icon: LineChart },
  { label: 'Logs', href: APP_ROUTES.modelGateway.logs, icon: Activity },
  { label: 'API Keys', href: APP_ROUTES.modelGateway.keys, icon: Key },
  { label: 'Setup', href: APP_ROUTES.modelGateway.setup, icon: Wrench },
];

// ─── Groups ───────────────────────────────────────────────────────────────────

export const NAV_GROUPS: NavGroup[] = [
  { label: 'Control Plane', items: MAIN_NAV },
  { label: 'Security', items: SECURITY_NAV },
  { label: 'Model Gateway', items: MODEL_GATEWAY_NAV },
  { label: 'System', items: SYSTEM_NAV },
];
