'use client';

/** @tier 3 · agents/[slug]-scoped. Detail page: identity header + four tabs
 * (docs/adr/2026-07-17-agent-detail-configuration.md §1). */

import { useState } from 'react';
import type { AgentOverviewResponse } from '@orbit/shared/api';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Callout,
  Heading,
  Icon,
  SegmentedControl,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
  EmptyState,
} from '@orbit/hades';
import Link from 'next/link';
import { StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { AgentOverviewTab } from './agent-overview-tab';
import { useAgentOverview } from '../_hooks/use-agent-overview';
import { DEFAULT_PERIOD, PERIOD_OPTIONS, isPeriod } from '../../_lib/spend';
import type { Period } from '../../_lib/spend';
import { AgentRunsTab } from './agent-runs-tab';
import { AgentConfigurationTab } from './agent-configuration-tab';
import { AgentTriggersTab } from './agent-triggers-tab';
import { StartRunModal } from '../../_components/start-run-modal';

interface AgentDetailPageProps {
  /** The default window's overview, fetched by the server render so first paint has data. */
  initialOverview?: AgentOverviewResponse | null | undefined;
}

export function AgentDetailPage({ initialOverview }: AgentDetailPageProps) {
  const { agent, status, error } = useAgentDetailContext();
  const router = useRouter();

  const [startRunOpen, setStartRunOpen] = useState(false);
  const [tab, setTab] = useState('overview');
  // Set when another tab sends the reader to a Configuration step; any tab click clears it.
  const [configSection, setConfigSection] = useState<string | undefined>();
  const [period, setPeriod] = useState<Period>(DEFAULT_PERIOD);
  const { overview, status: overviewStatus } = useAgentOverview(
    agent?.slug,
    period,
    initialOverview,
  );

  // ── Not found ──────────────────────────────────────────────────────────────

  if (status === 'not_found') {
    return (
      <Stack gap="comfortable">
        <Stack direction="horizontal">
          <Button variant="ghost" size="sm" asChild>
            <Link href={APP_ROUTES.agents.list}>
              <Icon name="arrow-left" size="xs" />
              Agents
            </Link>
          </Button>
        </Stack>
        <EmptyState
          icon="search-x"
          title="Agent not found"
          description="This agent definition does not exist or may have been deleted."
          action={
            <Button asChild>
              <Link href={APP_ROUTES.agents.list}>Back to Agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  // ── Error fallback ─────────────────────────────────────────────────────────

  if (status === 'error' && !agent) {
    return (
      <Stack gap="comfortable">
        <Stack direction="horizontal">
          <Button variant="ghost" size="sm" asChild>
            <Link href={APP_ROUTES.agents.list}>
              <Icon name="arrow-left" size="xs" />
              Agents
            </Link>
          </Button>
        </Stack>
        <EmptyState
          icon="triangle-alert"
          title="Failed to load agent"
          description={error ?? 'An unexpected error occurred.'}
          action={
            <Button asChild>
              <Link href={APP_ROUTES.agents.list}>Back to Agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  if (!agent) return null;

  const statusMap: Record<string, 'healthy' | 'degraded' | 'error'> = {
    active: 'healthy',
    provisioning: 'degraded',
    deleting: 'error',
  };
  const labelMap: Partial<Record<string, string>> = {
    provisioning: 'Provisioning',
    deleting: 'Deleting',
  };

  return (
    <Stack gap="comfortable">
      {/* Provisioning banner */}
      {agent.state === 'provisioning' && (
        <Callout variant="info">
          Agent is being provisioned, this usually takes 30–60 seconds.
        </Callout>
      )}

      {/* ── Identity header (builder motif) ─────────────────────────────────── */}
      <Stack direction="horizontal" align="center" justify="between" wrap gap="comfortable">
        <Stack direction="horizontal" align="start" gap="cozy">
          <Icon name="bot" size="lg" color="accent" container containerVariant="accent" label="" />
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" gap="snug" wrap>
              <Heading variant="title">{agent.name}</Heading>
              <StatusBadge
                status={statusMap[agent.state] ?? 'unknown'}
                label={labelMap[agent.state]}
              />
            </Stack>
            <Stack direction="horizontal" align="center" gap="snug" wrap>
              <Text as="span" mono size="supporting" color="subtle">
                {agent.slug}
              </Text>
              <Text as="span" size="supporting" color="subtle">
                ·
              </Text>
              <Stack direction="horizontal" align="center" gap="tight">
                <Icon name="cpu" size="xs" color="subtle" label="" />
                <Text as="span" mono size="supporting" color="subtle">
                  {agent.modelId}
                </Text>
              </Stack>
              <Text as="span" size="supporting" color="subtle">
                ·
              </Text>
              <Stack direction="horizontal" align="center" gap="tight">
                <Icon name="square-terminal" size="xs" color="subtle" label="" />
                <Text as="span" size="supporting" color="subtle">
                  {agent.framework}
                </Text>
              </Stack>
              <Text as="span" size="supporting" color="subtle">
                ·
              </Text>
              {/* Identity model — always shown; delegated gets badge emphasis
                  because it changes who can start the agent and who sees runs. */}
              {agent.identityMode === 'delegated' ? (
                <Badge variant="default" size="sm">
                  Delegated
                </Badge>
              ) : (
                <Stack direction="horizontal" align="center" gap="tight">
                  <Icon name="bot" size="xs" color="subtle" label="" />
                  <Text as="span" size="supporting" color="subtle">
                    Autonomous
                  </Text>
                </Stack>
              )}
            </Stack>
          </Stack>
        </Stack>
        <Stack direction="horizontal" align="center" gap="snug">
          {tab === 'overview' && (
            <SegmentedControl
              aria-label="Period"
              value={period}
              onChange={(value) => isPeriod(value) && setPeriod(value)}
              options={PERIOD_OPTIONS}
            />
          )}
          <Button
            variant="default"
            size="sm"
            onClick={() => setStartRunOpen(true)}
            disabled={agent.state !== 'active'}
          >
            <Icon name="play" size="xs" />
            Start Run
          </Button>
        </Stack>
      </Stack>

      {/* Tabs */}
      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value);
          setConfigSection(undefined);
        }}
      >
        <TabsList>
          <TabsTrigger value="overview" icon="activity">
            Overview
          </TabsTrigger>
          <TabsTrigger value="runs" icon="play">
            Runs
          </TabsTrigger>
          <TabsTrigger value="configuration" icon="edit">
            Configuration
          </TabsTrigger>
          <TabsTrigger value="triggers" icon="zap">
            Triggers
          </TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <AgentOverviewTab
            overview={overview}
            status={overviewStatus}
            onEditInstructions={() => {
              setConfigSection('behaviour');
              setTab('configuration');
            }}
          />
        </TabsContent>
        <TabsContent value="runs">
          <AgentRunsTab onStartRun={() => setStartRunOpen(true)} />
        </TabsContent>
        <TabsContent value="configuration">
          <AgentConfigurationTab initialSection={configSection} />
        </TabsContent>
        <TabsContent value="triggers">
          <AgentTriggersTab />
        </TabsContent>
      </Tabs>

      {/* Start run modal */}
      <StartRunModal
        open={startRunOpen}
        agentType={agent}
        onClose={() => setStartRunOpen(false)}
        onStarted={(runId, agentSlug) => {
          setStartRunOpen(false);
          router.push(APP_ROUTES.agents.run(agentSlug, runId));
        }}
      />
    </Stack>
  );
}
