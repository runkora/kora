'use client';

/** @tier 3 — dashboard-scoped. Uses AgentTypeRow, RunRow, ConnectionRow, EnvironmentRow. */

import Link from 'next/link';
import {
  Button,
  Card,
  Icon,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  Grid,
  Heading,
  List,
  Progress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
} from '@orbit/terra';
import { StatusBadge } from '@orbit/components';
import { MetricCard } from '@orbit/components';
import { EventFeedItem } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';

import type { AgentTypeRow, RunRow } from '../agents/_types';
import { formatRelativeTime } from '../agents/_types';
import type { ConnectionRow } from '../connections/_types';
import type { EnvironmentRow } from '../environments/_types';
import type { AnalyticsResponse, TenantCapStatus } from '../model-gateway/_types';
import { AnalyticsKPICards } from '../model-gateway/_components/analytics/analytics-kpi-cards';
import { AnalyticsRequestsChart } from '../model-gateway/_components/analytics/analytics-requests-chart';

// ─── Status maps ─────────────────────────────────────────────────────────────

const agentStatusMap: Record<string, 'healthy' | 'degraded' | 'error'> = {
  active: 'healthy',
  provisioning: 'degraded',
  deleting: 'error',
};

const agentLabelMap: Partial<Record<string, string>> = {
  provisioning: 'Provisioning',
  deleting: 'Deleting',
};

const runStatusMap: Record<string, 'healthy' | 'degraded' | 'error' | 'idle' | 'pending'> = {
  active: 'healthy',
  starting: 'pending',
  pending: 'pending',
  completing: 'degraded',
  completed: 'idle',
  failed: 'error',
  budget_exhausted: 'error',
  idle: 'idle',
};

// ─── Helpers ────────────────────────────────────────────────────────────────

/** Currency formatter for budget figures (two decimals — these are real dollars). */
function fmtBudgetUsd(n: number): string {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

// ─── Spend vs budget ──────────────────────────────────────────────────────────

function SpendBudgetCard({ cap }: { cap: TenantCapStatus | null }) {
  return (
    <Card column>
      <CardHeader>
        <Stack gap="tight">
          <CardTitle>Spend vs budget</CardTitle>
          <Text size="supporting" color="muted">
            Tenant spend against the configured cap.
          </Text>
        </Stack>
      </CardHeader>
      <CardContent grow>
        {cap == null || cap.limit_usd <= 0 ? (
          <Stack align="center" justify="center" paddingBlock="spacious" gap="snug">
            <Icon name="dollar-sign" size="lg" color="subtle" aria-hidden />
            <Text color="subtle">No tenant budget configured.</Text>
          </Stack>
        ) : (
          <Stack gap="relaxed">
            <Stack direction="horizontal" align="baseline" justify="between">
              <Heading variant="metric">{fmtBudgetUsd(cap.spend_usd)}</Heading>
              <Text size="supporting" color="muted">
                of {fmtBudgetUsd(cap.limit_usd)}
              </Text>
            </Stack>
            <Progress
              value={cap.spend_usd}
              max={cap.limit_usd}
              variant={
                cap.reached
                  ? 'error'
                  : cap.spend_usd / cap.limit_usd >= 0.8
                    ? 'degraded'
                    : 'healthy'
              }
              size="lg"
              label="Tenant spend usage"
              showValue
            />
            <Stack direction="horizontal" align="center" justify="between">
              <StatusBadge
                status={cap.reached ? 'error' : 'healthy'}
                label={cap.reached ? 'Limit reached' : 'Within budget'}
              />
              <Text size="caption" color="subtle">
                Resets {cap.reset_cadence}
              </Text>
            </Stack>
          </Stack>
        )}
      </CardContent>
    </Card>
  );
}

// ─── Types ────────────────────────────────────────────────────────────────────

export interface DashboardPageProps {
  agentTypes: AgentTypeRow[];
  runs: RunRow[];
  connections: ConnectionRow[];
  environments: EnvironmentRow[];
  analytics: AnalyticsResponse | null;
  tenantCap: TenantCapStatus | null;
  windowDays?: number;
}

// ─── DashboardPage ────────────────────────────────────────────────────────────

export function DashboardPage({
  agentTypes,
  runs,
  connections,
  environments,
  analytics,
  tenantCap,
  windowDays = 7,
}: DashboardPageProps) {
  const activeRunCount = runs.filter((r) =>
    ['active', 'starting', 'pending', 'completing'].includes(r.state),
  ).length;

  const recentRuns = runs.slice(0, 8);
  const displayedAgents = agentTypes.slice(0, 6);

  return (
    <Stack gap="relaxed" asChild>
      <main className="px-6 py-6 max-w-screen-xl mx-auto">
        <Stack gap="tight">
          <Heading variant="title">Dashboard</Heading>
          <Text color="muted">Real-time overview of the Orbit control plane.</Text>
        </Stack>

        {/* ── Inventory stat cards ── */}
        <Grid cols={{ base: 2, lg: 4 }} gap="comfortable">
          <MetricCard
            icon={
              <Icon name="bot" size="sm" container containerVariant="surface-subtle" aria-hidden />
            }
            label="Agent Types"
            value={String(agentTypes.length)}
            delta={{ text: 'registered', positive: true }}
            href={APP_ROUTES.agents.list}
          />
          <MetricCard
            icon={
              <Icon
                name="activity"
                size="sm"
                container
                containerVariant="surface-subtle"
                aria-hidden
              />
            }
            label="Active Runs"
            value={String(activeRunCount)}
            delta={{ text: `${runs.length} total`, positive: activeRunCount > 0 }}
            href={APP_ROUTES.agents.list}
          />
          <MetricCard
            icon={
              <Icon
                name="network"
                size="sm"
                container
                containerVariant="surface-subtle"
                aria-hidden
              />
            }
            label="Connections"
            value={String(connections.length)}
            delta={{ text: 'configured', positive: true }}
            href={APP_ROUTES.connections}
          />
          <MetricCard
            icon={
              <Icon
                name="layers"
                size="sm"
                container
                containerVariant="surface-subtle"
                aria-hidden
              />
            }
            label="Environments"
            value={String(environments.length)}
            delta={{ text: 'configured', positive: true }}
            href={APP_ROUTES.environments}
          />
        </Grid>

        {/* ── Agent types table ── */}
        <Card>
          <CardHeader>
            <Stack direction="horizontal" align="center" justify="between">
              <Stack gap="tight">
                <CardTitle>Agent Types</CardTitle>
                <Text size="supporting" color="muted">
                  All agent types registered with the control plane.
                </Text>
              </Stack>
              <Button variant="outline" size="sm" asChild>
                <Link href={APP_ROUTES.agents.list}>
                  View all
                  <Icon name="chevron-right" size="2xs" />
                </Link>
              </Button>
            </Stack>
          </CardHeader>
          <CardContent flush>
            {agentTypes.length === 0 ? (
              <Stack align="center" justify="center" paddingBlock="spacious">
                <Icon name="bot" size="lg" color="subtle" aria-hidden />
                <Text color="subtle">No agent types registered yet.</Text>
              </Stack>
            ) : (
              <TableRoot>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Model</TableHead>
                      <TableHead>Created</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {displayedAgents.map((agent) => (
                      <TableRow key={agent.id}>
                        <TableCell>
                          <Stack gap="none">
                            <Text size="supporting" weight="emphasis" mono>
                              {agent.name}
                            </Text>
                            <Text size="caption" color="subtle" mono>
                              {agent.slug}
                            </Text>
                          </Stack>
                        </TableCell>
                        <TableCell>
                          <StatusBadge
                            status={agentStatusMap[agent.state] ?? 'unknown'}
                            label={agentLabelMap[agent.state]}
                          />
                        </TableCell>
                        <TableCell>
                          <Text size="supporting" color="muted" mono>
                            {agent.modelId}
                          </Text>
                        </TableCell>
                        <TableCell>
                          <Text size="supporting" color="muted">
                            {formatRelativeTime(agent.createdAt)}
                          </Text>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableRoot>
            )}
          </CardContent>
          {agentTypes.length > 0 && (
            <CardFooter justify="between" compact>
              <Text size="caption" color="subtle">
                Showing {displayedAgents.length} of {agentTypes.length} agent types
              </Text>
              <Button variant="ghost" size="sm" asChild>
                <Link href={APP_ROUTES.agents.list}>
                  View all
                  <Icon name="chevron-right" size="2xs" />
                </Link>
              </Button>
            </CardFooter>
          )}
        </Card>

        {/* ── Inference (model gateway) ── */}
        {analytics && (
          <Stack gap="comfortable">
            <Stack direction="horizontal" align="center" justify="between">
              <Stack gap="tight">
                <Heading variant="section">Inference</Heading>
                <Text size="supporting" color="muted">
                  Model gateway activity over the last {windowDays} days.
                </Text>
              </Stack>
              <Button variant="outline" size="sm" asChild>
                <Link href={APP_ROUTES.modelGateway.analytics}>
                  Analytics
                  <Icon name="chevron-right" size="2xs" />
                </Link>
              </Button>
            </Stack>
            <AnalyticsKPICards kpis={analytics.kpis} />
            <AnalyticsRequestsChart buckets={analytics.buckets} />
          </Stack>
        )}

        {/* ── Activity & spend ── */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          {/* Recent Runs */}
          <Card column>
            <CardHeader>
              <Stack gap="tight">
                <CardTitle>Recent Runs</CardTitle>
                <Text size="supporting" color="muted">
                  Latest agent run activity.
                </Text>
              </Stack>
            </CardHeader>
            <CardContent flush grow>
              {recentRuns.length === 0 ? (
                <Stack align="center" justify="center" paddingBlock="spacious">
                  <Icon name="activity" size="lg" color="subtle" aria-hidden />
                  <Text color="subtle">No runs yet.</Text>
                </Stack>
              ) : (
                <List separator>
                  {recentRuns.map((run) => (
                    <EventFeedItem
                      key={run.id}
                      status={runStatusMap[run.state] ?? 'idle'}
                      label={run.agentTypeName}
                      meta={run.state}
                      message={`Run ${run.id}`}
                      timestamp={run.createdAt ? formatRelativeTime(run.createdAt) : '—'}
                    />
                  ))}
                </List>
              )}
            </CardContent>
            <CardFooter justify="center" compact>
              <Button variant="ghost" size="sm" asChild>
                <Link href={APP_ROUTES.agents.list}>
                  View agents
                  <Icon name="chevron-right" size="2xs" />
                </Link>
              </Button>
            </CardFooter>
          </Card>

          {/* Spend vs budget */}
          <SpendBudgetCard cap={tenantCap} />
        </Grid>
      </main>
    </Stack>
  );
}
