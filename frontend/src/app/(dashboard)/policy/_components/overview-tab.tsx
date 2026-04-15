'use client';

import {
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
} from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import Link from 'next/link';
import { FileCheck2, Library, Route, Shield, ShieldCheck, UserCheck } from 'lucide-react';
import type { StatsResponse } from '../_types';

interface OverviewTabProps {
  stats: StatsResponse | null;
}

export function OverviewTab({ stats }: OverviewTabProps) {
  if (!stats) {
    return (
      <Stack gap="comfortable">
        <Heading variant="title">Policy Engine</Heading>
        <Card column>
          <CardContent>
            <Text color="muted">
              Unable to load policy stats. The policy service may be unavailable.
            </Text>
          </CardContent>
        </Card>
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
    <Stack direction="vertical" gap="comfortable" asChild>
      <section>
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
          <KPICard
            icon={<ShieldCheck className="size-4" />}
            label="Grants"
            value={stats.grants.total}
            href={APP_ROUTES.policy.grants}
            breakdown={Object.entries(stats.grants.by_principal_type).map(([type, count]) => ({
              label: type,
              value: count,
            }))}
          />
          <KPICard
            icon={<UserCheck className="size-4" />}
            label="Roles"
            value={stats.roles.total}
            href={APP_ROUTES.policy.roles}
            breakdown={[
              { label: 'Reserved', value: stats.roles.reserved },
              { label: 'Custom', value: stats.roles.custom },
            ]}
          />
          <KPICard
            icon={<Library className="size-4" />}
            label="Vocabulary"
            value={stats.vocabulary.total}
            href={APP_ROUTES.policy.vocabulary}
            breakdown={Object.entries(stats.vocabulary.by_resource_type).map(([type, count]) => ({
              label: type,
              value: count,
            }))}
          />
          <KPICard
            icon={<Route className="size-4" />}
            label="Routes"
            value={stats.routes.total}
            href={APP_ROUTES.policy.routes}
          />
        </Grid>

        {/* Bundle Status */}
        {stats.bundles.tenants.length > 0 && (
          <Card column>
            <CardHeader compact icon={<FileCheck2 className="size-4" />}>
              <CardTitle>Bundle Status</CardTitle>
            </CardHeader>
            <CardContent>
              <Stack gap="snug">
                {stats.bundles.tenants.map((tenant) => (
                  <Stack
                    key={tenant.tenant_id}
                    direction="horizontal"
                    align="center"
                    justify="between"
                  >
                    <Stack direction="horizontal" align="center" gap="snug">
                      <Shield className="size-3.5 text-subtle" />
                      <Text size="caption" mono>
                        {tenant.tenant_id}
                      </Text>
                    </Stack>
                    <Stack direction="horizontal" align="center" gap="snug">
                      <Text size="caption" color="subtle">
                        {formatTimestamp(tenant.last_compiled_at)}
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
              </Stack>
            </CardContent>
          </Card>
        )}

        {/* Quick Links */}
        <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
          <QuickLinkCard
            icon={<ShieldCheck className="size-4" />}
            title="Manage Grants"
            description="Create and revoke permission grants for agents, humans, and services."
            href={APP_ROUTES.policy.grants}
          />
          <QuickLinkCard
            icon={<UserCheck className="size-4" />}
            title="Manage Roles"
            description="Map OIDC roles to platform scopes for role-based access control."
            href={APP_ROUTES.policy.roles}
          />
        </Grid>
      </section>
    </Stack>
  );
}

// ── Sub-components ───────────────────────────────────────────────────────────

interface KPICardProps {
  icon: React.ReactNode;
  label: string;
  value: number;
  href: string;
  breakdown?: { label: string; value: number }[];
}

function KPICard({ icon, label, value, href, breakdown }: KPICardProps) {
  return (
    <Card column>
      <CardHeader compact icon={icon}>
        <CardTitle>{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Heading variant="metric">{value}</Heading>
          {breakdown && breakdown.length > 0 && (
            <>
              <Separator />
              <Stack gap="tight">
                {breakdown.map((item) => (
                  <Stack key={item.label} direction="horizontal" align="center" justify="between">
                    <Text size="caption" color="muted">
                      {item.label}
                    </Text>
                    <Text size="caption" weight="emphasis">
                      {item.value}
                    </Text>
                  </Stack>
                ))}
              </Stack>
            </>
          )}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <span />
        <Link href={href} className="text-xs text-subtle hover:text-foreground transition-colors">
          View all →
        </Link>
      </CardFooter>
    </Card>
  );
}

interface QuickLinkCardProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  href: string;
}

function QuickLinkCard({ icon, title, description, href }: QuickLinkCardProps) {
  return (
    <Link href={href} className="block transition-colors [&>*]:hover:border-accent/40">
      <Card column>
        <CardHeader compact icon={icon}>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          <Text size="caption" color="muted">
            {description}
          </Text>
        </CardContent>
      </Card>
    </Link>
  );
}

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return iso;
  }
}
