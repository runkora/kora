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
import { HealthStatus } from '@orbit/components';
import { StatusBadge } from '@orbit/components';
import { MetricCard } from '@orbit/components';
import { EventFeedItem } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';

import type { AgentTypeRow, RunRow } from '../agents/_types';
import { formatRelativeTime } from '../agents/_types';
import type { ConnectionRow } from '../connections/_types';
import type { EnvironmentRow } from '../environments/_types';

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

// ─── Types ────────────────────────────────────────────────────────────────────

export interface DashboardPageProps {
  agentTypes: AgentTypeRow[];
  runs: RunRow[];
  connections: ConnectionRow[];
  environments: EnvironmentRow[];
}

// ─── DashboardPage ────────────────────────────────────────────────────────────

export function DashboardPage({ agentTypes, runs, connections, environments }: DashboardPageProps) {
  const activeRunCount = runs.filter((r) =>
    ['active', 'starting', 'pending', 'completing'].includes(r.state),
  ).length;

  const recentRuns = runs.slice(0, 10);
  const displayedAgents = agentTypes.slice(0, 6);

  return (
    <Stack gap="spacious" asChild>
      <main className="px-6 py-8 max-w-screen-xl mx-auto">
        <Stack gap="tight">
          <Heading variant="title">Dashboard</Heading>
          <Text color="muted">Real-time overview of the Orbit control plane.</Text>
        </Stack>

        <HealthStatus />

        {/* ── Stat cards ── */}
        <Grid cols={{ base: 2, lg: 4 }} gap="comfortable">
          <MetricCard
            icon={
              <Icon name="bot" size="sm" container containerVariant="surface-subtle" aria-hidden />
            }
            label="Agent Types"
            value={String(agentTypes.length)}
            delta={{ text: 'registered', positive: true }}
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
            delta={{ text: 'currently running', positive: activeRunCount > 0 }}
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

        {/* ── Bottom row ── */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          {/* Recent Runs */}
          <Card column>
            <CardHeader>
              <Stack direction="horizontal" align="center" justify="between">
                <Stack gap="tight">
                  <CardTitle>Recent Runs</CardTitle>
                  <Text size="supporting" color="muted">
                    Latest agent run activity.
                  </Text>
                </Stack>
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

          {/* Platform Sections */}
          <Card column>
            <CardHeader>
              <Stack gap="tight">
                <CardTitle>Platform</CardTitle>
                <Text size="supporting" color="muted">
                  Quick links to platform sections.
                </Text>
              </Stack>
            </CardHeader>
            <CardContent grow>
              <Stack gap="comfortable">
                <Button variant="outline" size="sm" asChild>
                  <Link href={APP_ROUTES.agents.list}>
                    <Icon name="bot" size="2xs" />
                    Agents
                    <Icon name="chevron-right" size="2xs" />
                  </Link>
                </Button>
                <Button variant="outline" size="sm" asChild>
                  <Link href={APP_ROUTES.connections}>
                    <Icon name="network" size="2xs" />
                    Connections
                    <Icon name="chevron-right" size="2xs" />
                  </Link>
                </Button>
                <Button variant="outline" size="sm" asChild>
                  <Link href={APP_ROUTES.modelGateway.models}>
                    <Icon name="sparkles" size="2xs" />
                    Model Gateway
                    <Icon name="chevron-right" size="2xs" />
                  </Link>
                </Button>
                <Button variant="outline" size="sm" asChild>
                  <Link href={APP_ROUTES.environments}>
                    <Icon name="layers" size="2xs" />
                    Environments
                    <Icon name="chevron-right" size="2xs" />
                  </Link>
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>
      </main>
    </Stack>
  );
}
