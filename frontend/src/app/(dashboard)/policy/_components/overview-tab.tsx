'use client';

import { useMemo } from 'react';
import {
  Alert,
  Badge,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  Grid,
  Heading,
  Separator,
  Stack,
  Text,
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  Icon,
} from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import { MetricCard } from '@orbit/components';
import { fmtDate, fmtDateTime } from '@orbit/utils';
import Link from 'next/link';
import type {
  Grant,
  RoleEntry,
  RouteRegistration,
  ScopeEntryWithUsage,
  StatsResponse,
} from '../_types';
import { ScopeChip } from './scope-chip';
import { QuickLinkCard } from './quick-link-card';

interface OverviewTabProps {
  stats: StatsResponse | null;
  recentGrants: Grant[];
  roles: RoleEntry[];
  vocabulary: ScopeEntryWithUsage[];
  routes: RouteRegistration[];
}

export function OverviewTab({ stats, recentGrants, roles, vocabulary, routes }: OverviewTabProps) {
  const scopeCoverage = useMemo(() => {
    const total = vocabulary.length;
    const used = vocabulary.filter((s) => s.grant_count > 0 || s.role_count > 0).length;
    const unused = total - used;
    return { total, used, unused, pct: total > 0 ? Math.round((used / total) * 100) : 0 };
  }, [vocabulary]);

  const totalRouteCount = useMemo(() => {
    return routes.reduce((sum, reg) => sum + reg.routes.length, 0);
  }, [routes]);

  const serviceCount = useMemo(() => {
    return new Set(routes.map((r) => r.service)).size;
  }, [routes]);

  if (!stats) {
    return (
      <Stack gap="comfortable">
        <Heading variant="title">Policy Engine</Heading>
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Text size="caption" color="status-error">Unable to load policy stats. The policy service may be unavailable.</Text>
        </Alert>
      </Stack>
    );
  }

  const bundleStatus =
    stats.bundles.tenants.length > 0
      ? stats.bundles.tenants.every((t) => t.status === 'current')
        ? 'current'
        : 'stale'
      : 'none';

  return (
    <Stack direction="vertical" gap="comfortable">
      {/* Page header */}
      <Stack gap="snug">
        <Stack direction="horizontal" align="center" gap="snug">
          <Heading variant="title">Policy Engine</Heading>
          <Badge
            variant={
              bundleStatus === 'current'
                ? 'healthy'
                : bundleStatus === 'stale'
                  ? 'degraded'
                  : 'outline'
            }
            dot
            size="sm"
          >
            {bundleStatus === 'current'
              ? 'Bundles Current'
              : bundleStatus === 'stale'
                ? 'Bundle Stale'
                : 'No Bundles'}
          </Badge>
        </Stack>
        <Text size="supporting" color="muted">
          Manage grants, roles, and authorization policies for your tenant.
        </Text>
      </Stack>

      {/* KPI Cards */}
      <Grid cols={{ base: 1, sm: 2, lg: 4 }} gap="comfortable">
        <MetricCard
          icon={<Icon name="shield-check" />}
          label="Grants"
          value={stats.grants.total}
          href={APP_ROUTES.policy.grants}
          breakdown={Object.entries(stats.grants.by_principal_type).map(([type, count]) => ({
            label: type,
            value: count,
          }))}
        />
        <MetricCard
          icon={<Icon name="user-check" />}
          label="Roles"
          value={stats.roles.total}
          href={APP_ROUTES.policy.roles}
          breakdown={[
            { label: 'Reserved', value: stats.roles.reserved },
            { label: 'Custom', value: stats.roles.custom },
          ]}
        />
        <MetricCard
          icon={<Icon name="library" />}
          label="Vocabulary"
          value={stats.vocabulary.total}
          href={APP_ROUTES.policy.vocabulary}
          breakdown={Object.entries(stats.vocabulary.by_resource_type).map(([type, count]) => ({
            label: type,
            value: count,
          }))}
        />
        <MetricCard
          icon={<Icon name="route" />}
          label="Routes"
          value={totalRouteCount}
          href={APP_ROUTES.policy.routes}
          breakdown={[
            { label: 'Services', value: serviceCount },
            { label: 'Endpoints', value: totalRouteCount },
          ]}
        />
      </Grid>

      {/* Middle row: Scope coverage + Bundle status */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        {/* Scope coverage */}
        <Card column>
          <CardHeader compact icon={<Icon name="library" />}>
            <CardTitle>Scope Coverage</CardTitle>
          </CardHeader>
          <CardContent>
            <Stack gap="snug">
              <Text size="caption" color="muted">
                Scopes assigned to at least one grant or role vs unassigned scopes in the
                vocabulary.
              </Text>
              <Separator />
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="caption" color="muted">
                  Assigned
                </Text>
                <Stack direction="horizontal" align="center" gap="snug">
                  <Text size="caption" weight="emphasis" mono tabular>
                    {scopeCoverage.used}
                  </Text>
                  <Text size="caption" color="subtle">
                    / {scopeCoverage.total}
                  </Text>
                </Stack>
              </Stack>
              <aside className="h-2 rounded-full bg-surface-elevated overflow-hidden">
                <aside
                  className="h-full rounded-full bg-accent transition-all"
                  style={{ width: `${scopeCoverage.pct}%` }}
                />
              </aside>
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="caption" color="subtle">
                  {scopeCoverage.pct}% coverage
                </Text>
                {scopeCoverage.unused > 0 && (
                  <Text size="caption" color="status-degraded">
                    {scopeCoverage.unused} unused
                  </Text>
                )}
              </Stack>
            </Stack>
          </CardContent>
          <CardFooter compact justify="end">
            <Link
              href={APP_ROUTES.policy.vocabulary}
              className="text-xs text-subtle hover:text-foreground transition-colors"
            >
              View vocabulary →
            </Link>
          </CardFooter>
        </Card>

        {/* Bundle Status */}
        <Card column>
          <CardHeader compact icon={<Icon name="file-check" />}>
            <CardTitle>Bundle Status</CardTitle>
          </CardHeader>
          <CardContent>
            <Stack gap="snug">
              <Text size="caption" color="muted">
                OPA policy bundles compiled per tenant. Bundles are recompiled on every grant, role,
                or route change.
              </Text>
              {stats.bundles.tenants.length > 0 ? (
                <>
                  <Separator />
                  {stats.bundles.tenants.map((tenant) => (
                    <Stack
                      key={tenant.tenant_id}
                      direction="horizontal"
                      align="center"
                      justify="between"
                    >
                      <Text size="caption" mono>
                        {tenant.tenant_id}
                      </Text>
                      <Stack direction="horizontal" align="center" gap="snug">
                        <Text size="caption" color="subtle">
                          {fmtDateTime(tenant.last_compiled_at)}
                        </Text>
                        <Badge
                          variant={tenant.status === 'current' ? 'healthy' : 'degraded'}
                          size="sm"
                        >
                          {tenant.status}
                        </Badge>
                      </Stack>
                    </Stack>
                  ))}
                </>
              ) : (
                <>
                  <Separator />
                  <Text size="caption" color="subtle">
                    No bundles compiled yet.
                  </Text>
                </>
              )}
            </Stack>
          </CardContent>
        </Card>
      </Grid>

      {/* Recent Grants */}
      {recentGrants.length > 0 && (
        <Card column>
          <CardHeader compact icon={<Icon name="shield-check" />}>
            <CardTitle>Recent Grants</CardTitle>
          </CardHeader>
          <CardContent flush>
            <TableRoot>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Principal</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Scopes</TableHead>
                    <TableHead>Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {recentGrants.map((grant) => (
                    <TableRow key={grant.id}>
                      <TableCell>
                        <Text size="caption" mono>
                          {grant.principal_id}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            grant.principal_type === 'AGENT'
                              ? 'default'
                              : grant.principal_type === 'HUMAN'
                                ? 'healthy'
                                : 'degraded'
                          }
                          size="sm"
                        >
                          {grant.principal_type}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" gap="tight" wrap>
                          {grant.scopes.slice(0, 3).map((s) => (
                            <ScopeChip key={s} scope={s} />
                          ))}
                          {grant.scopes.length > 3 && (
                            <Text size="caption" color="subtle">
                              +{grant.scopes.length - 3} more
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Text size="caption" color="subtle">
                          {fmtDate(grant.created_at)}
                        </Text>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableRoot>
          </CardContent>
          <CardFooter compact justify="end">
            <Link
              href={APP_ROUTES.policy.grants}
              className="text-xs text-subtle hover:text-foreground transition-colors"
            >
              View all grants →
            </Link>
          </CardFooter>
        </Card>
      )}

      {/* Role summary */}
      {roles.length > 0 && (
        <Card column>
          <CardHeader compact icon={<Icon name="user-check" />}>
            <CardTitle>Roles</CardTitle>
          </CardHeader>
          <CardContent>
            <Stack gap="snug">
              <Text size="caption" color="muted">
                OIDC role→scope mappings. Reserved roles are maintained by the platform.
              </Text>
              <Separator />
              <Stack direction="horizontal" gap="tight" wrap>
                {roles.map((role) => {
                  const isReserved = role.role === 'tenant-admin' || role.role === 'platform-admin';
                  return (
                    <Badge
                      key={role.id}
                      shape="pill"
                      size="lg"
                      variant={isReserved ? 'reserved' : 'elevated'}
                      mono
                    >
                      {role.role}
                      <Text size="micro" color="subtle" mono={false}>
                        {(role.scopes ?? []).length}
                      </Text>
                    </Badge>
                  );
                })}
              </Stack>
            </Stack>
          </CardContent>
          <CardFooter compact justify="end">
            <Link
              href={APP_ROUTES.policy.roles}
              className="text-xs text-subtle hover:text-foreground transition-colors"
            >
              Manage roles →
            </Link>
          </CardFooter>
        </Card>
      )}

      {/* Quick Links */}
      <Grid cols={{ base: 1, sm: 2, lg: 4 }} gap="comfortable">
        <QuickLinkCard
          icon={<Icon name="shield-check" />}
          title="Manage Grants"
          description="Create and revoke permission grants for agents, humans, and services."
          href={APP_ROUTES.policy.grants}
        />
        <QuickLinkCard
          icon={<Icon name="user-check" />}
          title="Manage Roles"
          description="Map OIDC roles to platform scopes for role-based access control."
          href={APP_ROUTES.policy.roles}
        />
        <QuickLinkCard
          icon={<Icon name="route" />}
          title="View Routes"
          description="See registered route→scope mappings across all services."
          href={APP_ROUTES.policy.routes}
        />
        <QuickLinkCard
          icon={<Icon name="flask" />}
          title="Evaluate"
          description="Look up effective scopes and dry-run policy decisions."
          href={APP_ROUTES.policy.evaluate}
        />
      </Grid>
    </Stack>
  );
}
