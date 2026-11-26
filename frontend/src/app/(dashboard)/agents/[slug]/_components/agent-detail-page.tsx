'use client';

/** @tier 3 · agents/[slug]-scoped. Detail page: identity header, the tabs and the Configure modal
 * (docs/adr/2026-07-17-agent-detail-configuration.md §1). The URL's `tab` picks the tab; on the
 * Runs tab `grader`, `question` and `verdict` filter runs by grade, and on the Grades tab
 * `grader` opens that Grader. */

import { useEffect, useMemo, useState } from 'react';
import type { AgentOverviewResponse } from '@runkora/shared/api';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
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
} from '@runkora/hades';
import Link from 'next/link';
import { ManagedInGitBadge, PermissionHint, StatusBadge } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { AgentOverviewTab } from './agent-overview-tab';
import { useAgentOverview } from '../_hooks/use-agent-overview';
import { DEFAULT_PERIOD, PERIOD_OPTIONS, isPeriod } from '../../_lib/spend';
import type { Period } from '../../_lib/spend';
import { AgentRunsTab } from './agent-runs-tab';
import { AgentConfigureModal } from './agent-configure-modal';
import { AgentTriggersTab } from './agent-triggers-tab';
import { useHasScope } from '../../../_components/permissions-provider';
import { AgentGradesTab } from './agent-grades';
import { gradeFilterOf } from '../../_lib/grades';

/** The tab, the open Grader and the runs' grade filter a URL names. Triggers needs `triggers:read`. */
function viewOf(params: URLSearchParams, canReadTriggers: boolean) {
  const named = params.get('tab') ?? '';
  const tabs = ['overview', 'runs', 'grades', ...(canReadTriggers ? ['triggers'] : [])];
  const tab = tabs.includes(named) ? named : 'overview';
  return {
    tab,
    grader: tab === 'grades' ? (params.get('grader') ?? undefined) : undefined,
    gradeFilter: tab === 'runs' ? gradeFilterOf(params) : null,
  };
}

interface AgentDetailPageProps {
  /** The default window's overview, fetched by the server render so first paint has data. */
  initialOverview?: AgentOverviewResponse | null | undefined;
}

export function AgentDetailPage({ initialOverview }: AgentDetailPageProps) {
  const { agent, status, error, setGradeFilter } = useAgentDetailContext();
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams()?.toString() ?? '';

  const startRun = () => agent && router.push(APP_ROUTES.chatWith(agent.slug));
  const canReadTriggers = useHasScope('triggers:read');
  // The URL holds the tab, the open Grader and the runs' grade filter.
  const { tab, grader, gradeFilter } = useMemo(
    () => viewOf(new URLSearchParams(search), canReadTriggers),
    [search, canReadTriggers],
  );
  // The hook skips a grade filter equal to the one it holds, so the same URL is not reloaded.
  useEffect(() => {
    setGradeFilter(gradeFilter);
  }, [gradeFilter, setGradeFilter]);
  // The history API changes only the query, which Next syncs into useSearchParams without a server
  // request. Opening or leaving a Grader adds a history entry, so Back returns to where the reader was.
  const show = (next: string, nextGrader?: string) => {
    const query = new URLSearchParams({ tab: next });
    if (nextGrader) query.set('grader', nextGrader);
    const href = `${pathname}?${query}`;
    if (next === 'grades' && nextGrader !== grader) window.history.pushState(null, '', href);
    else window.history.replaceState(null, '', href);
  };
  const setTab = (next: string) => show(next);
  // The Configure modal, and the rail step it opens on.
  const [configureOpen, setConfigureOpen] = useState(false);
  const [configureSection, setConfigureSection] = useState<string | undefined>();
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
              {agent.declared && <ManagedInGitBadge repository={agent.repository} />}
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
            variant="outline"
            size="sm"
            onClick={() => {
              setConfigureSection(undefined);
              setConfigureOpen(true);
            }}
          >
            <Icon name="sliders-horizontal" size="xs" />
            Configure
          </Button>
          <PermissionHint allowed={agent.canRun} action="start runs of this agent">
            <Button
              variant="default"
              size="sm"
              onClick={startRun}
              disabled={!agent.canRun || agent.state !== 'active'}
            >
              <Icon name="play" size="xs" />
              Start Run
            </Button>
          </PermissionHint>
        </Stack>
      </Stack>

      {/* Tabs */}
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="overview" icon="activity">
            Overview
          </TabsTrigger>
          <TabsTrigger value="runs" icon="play">
            Runs
          </TabsTrigger>
          <TabsTrigger value="grades" icon="file-check">
            Grades
          </TabsTrigger>
          {canReadTriggers && (
            <TabsTrigger value="triggers" icon="zap">
              Triggers
            </TabsTrigger>
          )}
        </TabsList>
        <TabsContent value="overview">
          <AgentOverviewTab
            overview={overview}
            status={overviewStatus}
            onEditInstructions={() => {
              setConfigureSection('behaviour');
              setConfigureOpen(true);
            }}
            onViewAllRuns={() => setTab('runs')}
          />
        </TabsContent>
        <TabsContent value="runs">
          <AgentRunsTab onStartRun={startRun} />
        </TabsContent>
        {canReadTriggers && (
          <TabsContent value="triggers">
            <AgentTriggersTab />
          </TabsContent>
        )}
        <TabsContent value="grades">
          <AgentGradesTab
            agentTypeId={agent.id}
            slug={agent.slug}
            grader={grader}
            onGrader={(name) => show('grades', name)}
          />
        </TabsContent>
      </Tabs>

      <AgentConfigureModal
        open={configureOpen}
        initialSection={configureSection}
        onClose={() => setConfigureOpen(false)}
      />
    </Stack>
  );
}
