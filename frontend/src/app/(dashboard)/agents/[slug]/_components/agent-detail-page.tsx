'use client';

/** @tier 3 — agents/[slug]-scoped. Detail page with header + tabs. */

import { useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Callout,
  Heading,
  Icon,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import Link from 'next/link';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { AgentOverviewTab } from './agent-overview-tab';
import { AgentRunsTab } from './agent-runs-tab';
import { AgentSettingsTab } from './agent-settings-tab';
import { StartRunModal } from '../../_components/start-run-modal';

export function AgentDetailPage() {
  const { agent, status, error, deleteAgent } = useAgentDetailContext();
  const router = useRouter();

  const [startRunOpen, setStartRunOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    setDeleteError(null);
    const ok = await deleteAgent();
    setDeleting(false);
    if (!ok) {
      setDeleteError('Failed to delete agent. Please try again.');
      return;
    }
    setDeleteOpen(false);
    router.push(APP_ROUTES.agents.list);
  }, [deleteAgent, router]);

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
  const frameworkVariantMap: Record<string, 'default' | 'outline' | 'secondary'> = {
    'claude-code': 'default',
    codex: 'outline',
    opencode: 'secondary',
  };

  return (
    <Stack gap="comfortable">
      {/* Breadcrumb */}
      <Stack direction="horizontal">
        <Button variant="ghost" size="sm" asChild>
          <Link href={APP_ROUTES.agents.list}>
            <Icon name="arrow-left" size="xs" />
            Agents
          </Link>
        </Button>
      </Stack>

      {/* Provisioning banner */}
      {agent.state === 'provisioning' && (
        <Callout variant="info">
          Agent is being provisioned, this usually takes 30–60 seconds.
        </Callout>
      )}

      {/* Page header */}
      <Stack direction="horizontal" align="center" justify="between" wrap gap="comfortable">
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" gap="snug" wrap>
            <Heading variant="title">{agent.name}</Heading>
            <Badge variant={frameworkVariantMap[agent.frameworkKey] ?? 'outline'} size="sm">
              {agent.framework}
            </Badge>
            <StatusBadge
              status={statusMap[agent.state] ?? 'unknown'}
              label={labelMap[agent.state]}
            />
          </Stack>
          <Text mono size="supporting" color="muted">
            {agent.slug}
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="snug">
          <Button
            variant="default"
            size="sm"
            onClick={() => setStartRunOpen(true)}
            disabled={agent.state !== 'active'}
          >
            <Icon name="play" size="xs" />
            Start Run
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setDeleteOpen(true)}>
            <Icon name="trash" size="xs" />
            Delete
          </Button>
        </Stack>
      </Stack>

      {/* Tabs */}
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="runs">Runs</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <AgentOverviewTab />
        </TabsContent>
        <TabsContent value="runs">
          <AgentRunsTab onStartRun={() => setStartRunOpen(true)} />
        </TabsContent>
        <TabsContent value="settings">
          <AgentSettingsTab />
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

      {/* Delete confirm */}
      <ConfirmDialog
        open={deleteOpen}
        onClose={() => {
          setDeleteOpen(false);
          setDeleteError(null);
        }}
        title="Delete agent"
        description={`Permanently delete "${agent.name}"? All associated runs will be terminated. This cannot be undone.`}
        confirmLabel="Delete agent"
        onConfirm={() => void handleDelete()}
        confirming={deleting}
        error={deleteError}
      />
    </Stack>
  );
}
