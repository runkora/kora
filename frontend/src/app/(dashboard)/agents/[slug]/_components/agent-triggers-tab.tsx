'use client';

/** @tier 3 — agents/[slug]-scoped. Lists and authors webhook triggers that fire
 * THIS agent, and shows recent deliveries that targeted it. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Alert, Badge, Button, Callout, DataTable, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge, TriggerFormModal } from '@orbit/components';
import type { TriggerFormValue } from '@orbit/components';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { Trigger, TriggerDelivery, WebhookSource } from '@orbit/shared/api';
import { useAgentDetailContext } from '../_context/agent-detail-context';

function statusTone(status: string): 'healthy' | 'degraded' | 'error' | 'unknown' {
  switch (status) {
    case 'dispatched':
      return 'healthy';
    case 'verified':
    case 'skipped':
      return 'degraded';
    case 'failed':
      return 'error';
    default:
      return 'unknown';
  }
}

function formatTimestamp(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

export function AgentTriggersTab() {
  const { agent } = useAgentDetailContext();

  const [sources, setSources] = useState<WebhookSource[]>([]);
  const [triggers, setTriggers] = useState<Trigger[]>([]);
  const [deliveries, setDeliveries] = useState<TriggerDelivery[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [formState, setFormState] = useState<{ trigger: TriggerFormValue | null } | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<Trigger | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const agentId = agent?.id ?? '';

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    setLoadError(null);
    try {
      const [srcRes, trgRes, delRes] = await Promise.all([
        fetch(ApiRoute.triggerSources.list(), { cache: 'no-store' }),
        fetch(ApiRoute.triggers.list(agentId), { cache: 'no-store' }),
        fetch(ApiRoute.triggerDeliveries.list(), { cache: 'no-store' }),
      ]);
      if (srcRes.ok) setSources(((await srcRes.json()) as { data: WebhookSource[] }).data ?? []);
      if (trgRes.ok) setTriggers(((await trgRes.json()) as { data: Trigger[] }).data ?? []);
      if (delRes.ok)
        setDeliveries(((await delRes.json()) as { data: TriggerDelivery[] }).data ?? []);
      if (!trgRes.ok) setLoadError('Failed to load triggers.');
    } catch {
      setLoadError('Network error — could not load triggers.');
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    void load();
  }, [load]);

  const triggerIds = useMemo(() => new Set(triggers.map((t) => t.id)), [triggers]);
  const agentDeliveries = useMemo(
    () =>
      deliveries
        .filter(
          (d) =>
            (d.trigger_id && triggerIds.has(d.trigger_id)) ||
            (d.runs ?? []).some((r) => triggerIds.has(r.trigger_id)),
        )
        .slice(0, 20),
    [deliveries, triggerIds],
  );
  const sourceName = useMemo(() => {
    const map = new Map(sources.map((s) => [s.id, s.name]));
    return (id: string) => map.get(id) ?? id;
  }, [sources]);

  const toggleTrigger = useCallback(
    async (t: Trigger) => {
      await fetch(ApiRoute.triggers.detail(t.id), {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !t.enabled }),
      });
      void load();
    },
    [load],
  );

  const confirmDelete = useCallback(async () => {
    if (!deleteCandidate) return;
    setDeleting(true);
    setDeleteError(null);
    const res = await fetch(ApiRoute.triggers.delete(deleteCandidate.id), { method: 'DELETE' });
    setDeleting(false);
    if (!res.ok) {
      setDeleteError('Failed to delete trigger.');
      return;
    }
    setDeleteCandidate(null);
    void load();
  }, [deleteCandidate, load]);

  const columns = useMemo<DataTableColumn<Trigger>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text size="supporting" weight="emphasis">
              {row.name}
            </Text>
            <Text size="caption" color="muted">
              {sourceName(row.source_id)}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        cell: (row) => (
          <Badge variant="elevated" size="sm">
            {row.event_type}
          </Badge>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: (row) => (
          <StatusBadge
            status={row.enabled ? 'healthy' : 'unknown'}
            label={row.enabled ? 'Enabled' : 'Disabled'}
          />
        ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '10rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" justify="end">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() =>
                setFormState({
                  trigger: {
                    id: row.id,
                    kind: row.kind ?? 'webhook',
                    name: row.name,
                    sourceId: row.source_id ?? '',
                    agentTypeId: row.agent_type_id,
                    agentSlug: row.agent_slug,
                    eventType: row.event_type ?? '',
                    matchCondition: row.match_condition ?? {},
                    promptTemplate: row.prompt_template,
                    scheduleConfig: row.schedule_config
                      ? { cronExpression: row.schedule_config.cron_expression, timezone: row.schedule_config.timezone }
                      : undefined,
                  },
                })
              }
              aria-label={`Edit ${row.name}`}
            >
              <Icon name="edit" size="2xs" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => void toggleTrigger(row)}
              aria-label={row.enabled ? `Disable ${row.name}` : `Enable ${row.name}`}
            >
              <Icon name={row.enabled ? 'eye-off' : 'eye'} size="2xs" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setDeleteCandidate(row)}
              aria-label={`Delete ${row.name}`}
            >
              <Icon name="trash" size="2xs" />
            </Button>
          </Stack>
        ),
      },
    ],
    [sourceName, toggleTrigger],
  );

  const deliveryColumns = useMemo<DataTableColumn<TriggerDelivery>[]>(
    () => [
      {
        id: 'received',
        header: 'Received',
        locked: true,
        cell: (row) => (
          <Text mono size="caption">
            {formatTimestamp(row.received_at)}
          </Text>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        cell: (row) => (
          <Text size="caption" color="muted">
            {row.event_type ?? '—'}
          </Text>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: (row) => <StatusBadge status={statusTone(row.status)} label={row.status} />,
      },
      {
        id: 'detail',
        header: 'Detail',
        cell: (row) =>
          (row.runs?.length ?? 0) > 1 ? (
            <Text size="caption" color="muted">
              {row.runs?.length} runs
            </Text>
          ) : row.run_id ? (
            <Text mono size="caption">
              run {row.run_id.slice(0, 8)}
            </Text>
          ) : (
            <Text size="caption" color="muted">
              {row.error ?? '—'}
            </Text>
          ),
      },
    ],
    [],
  );

  if (!agent) return null;

  const agentOption = { id: agent.id, slug: agent.slug, name: agent.name };

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Heading variant="section">Triggers</Heading>
          <Text size="supporting" color="muted">
            Run this agent automatically when a matching event arrives on a webhook source. Managed
            by platform admins.
          </Text>
        </Stack>
        <Button
          size="sm"
          onClick={() => setFormState({ trigger: null })}
          disabled={sources.length === 0}
        >
          <Icon name="plus" size="xs" />
          New trigger
        </Button>
      </Stack>

      {loadError && <Alert variant="error">{loadError}</Alert>}

      {sources.length === 0 && !loading && (
        <Callout variant="info">
          No webhook sources exist yet. Register one in{' '}
          <Link href={APP_ROUTES.triggers}>Triggers</Link> before creating a trigger.
        </Callout>
      )}

      {!loading && triggers.length === 0 ? (
        <EmptyState
          icon="zap"
          title="No triggers for this agent"
          description="Create a trigger to run this agent automatically from a webhook event."
          action={
            <Button
              size="sm"
              onClick={() => setFormState({ trigger: null })}
              disabled={sources.length === 0}
            >
              <Icon name="plus" size="xs" />
              New trigger
            </Button>
          }
        />
      ) : (
        triggers.length > 0 && (
          <DataTable
            data={triggers}
            columns={columns}
            getRowKey={(row) => row.id}
            noun="triggers"
            searchPlaceholder="Search triggers…"
            searchFn={(row, q) =>
              row.name.toLowerCase().includes(q.toLowerCase()) ||
              row.event_type.toLowerCase().includes(q.toLowerCase())
            }
            defaultSortId="name"
            defaultSortDir="asc"
            emptyMessage="No triggers."
            filteredEmptyMessage="No triggers match your search."
          />
        )
      )}

      {agentDeliveries.length > 0 && (
        <Stack gap="snug">
          <Heading variant="section">Recent deliveries</Heading>
          <DataTable
            data={agentDeliveries}
            columns={deliveryColumns}
            getRowKey={(row) => row.id}
            noun="deliveries"
            emptyMessage="No deliveries."
          />
        </Stack>
      )}

      {formState && (
        <TriggerFormModal
          key={formState.trigger?.id ?? 'create'}
          open
          trigger={formState.trigger}
          sources={sources.map((s) => ({ id: s.id, name: s.name }))}
          agents={[agentOption]}
          lockedAgent={agentOption}
          onClose={() => setFormState(null)}
          onSaved={() => {
            setFormState(null);
            void load();
          }}
        />
      )}

      <ConfirmDialog
        open={!!deleteCandidate}
        title="Delete trigger"
        description={`Permanently delete "${deleteCandidate?.name ?? ''}"? It will stop firing runs for this agent.`}
        confirmLabel="Delete trigger"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void confirmDelete()}
        onClose={() => {
          setDeleteCandidate(null);
          setDeleteError(null);
        }}
      />
    </Stack>
  );
}
