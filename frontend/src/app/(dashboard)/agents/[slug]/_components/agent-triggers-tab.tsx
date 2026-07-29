'use client';

/** @tier 3 — agents/[slug]-scoped. Lists and authors webhook triggers that fire
 * THIS agent, and shows recent deliveries that targeted it. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Alert, Badge, Button, Callout, DataTable, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import { TriggerWizard } from '../../../triggers/_components/trigger-wizard';
import type {
  TriggerCreateResult,
  TriggerDraft,
} from '../../../triggers/_components/trigger-wizard';
import { createFromWizardDraft } from '../../../triggers/_services/create-from-wizard';
import { updateTrigger } from '../../../triggers/_services/triggers-api';
import { toTriggerUpdateRequest } from '../../../triggers/_types';
import { ingestHostFromWindow } from '../../../triggers/_lib/endpoints';
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

  const [wizardOpen, setWizardOpen] = useState(false);
  const ingestHost = ingestHostFromWindow();
  const [deleteCandidate, setDeleteCandidate] = useState<Trigger | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [toggleError, setToggleError] = useState<string | null>(null);

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
      setLoadError('Network error: could not load triggers.');
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

  /**
   * Enables or disables a trigger through the Triggers page's own client and
   * mapper, so a refusal here reads the same as a refusal there.
   *
   * The hand-rolled PATCH this replaced ignored the response: a toggle the API
   * refused (no permission, a delegated agent) reloaded the list, put the switch
   * back where it started, and said nothing about why.
   */
  const toggleTrigger = useCallback(
    async (t: Trigger) => {
      setToggleError(null);
      const result = await updateTrigger(t.id, toTriggerUpdateRequest({ enabled: !t.enabled }));
      if (result.error) {
        setToggleError(`${t.name}: ${result.error}`);
        return;
      }
      void load();
    },
    [load],
  );

  /**
   * Creates from the wizard's draft, then reloads this page's lists.
   *
   * The orchestration is the Triggers page's, shared rather than reimplemented:
   * a trigger created here is created exactly as one created there.
   */
  const createTriggerForAgent = useCallback(
    async (draft: TriggerDraft): Promise<TriggerCreateResult> => {
      const result = await createFromWizardDraft(draft, { ingestHost });
      if (result.ok) void load();
      return result;
    },
    [ingestHost, load],
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
              {row.source_id ? sourceName(row.source_id) : row.kind}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        cell: (row) => (
          <Badge variant="elevated" size="sm">
            {row.event_type ?? '—'}
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
            {/* Editing a trigger lives in the Triggers page drawer, which owns
                every field. This page creates and lists; it does not fork a
                second editor that would drift from it. */}
            <Button variant="ghost" size="icon-sm" asChild>
              <Link href={APP_ROUTES.triggers} aria-label={`Edit ${row.name} in Triggers`}>
                <Icon name="edit" size="2xs" />
              </Link>
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

  // Delegated agents cannot be bound to automated sources at all — explain the
  // constraint here instead of offering a form that would be refused on submit.
  if (agent.identityMode === 'delegated') {
    return (
      <Stack gap="comfortable">
        <Stack gap="tight">
          <Heading variant="section">Triggers</Heading>
          <Text size="supporting" color="muted">
            Run this agent automatically when a matching event arrives on a webhook source.
          </Text>
        </Stack>
        <EmptyState
          icon="user-check"
          title="Delegated agents cannot be triggered"
          description="This agent acts on behalf of the person who runs it, so only a person can start it. Triggers and other automated sources cannot be bound to it. To automate this work, create an autonomous agent instead."
        />
      </Stack>
    );
  }

  const agentOption = {
    id: agent.id,
    slug: agent.slug,
    name: agent.name,
    identityMode: agent.identityMode,
  };

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
        <Button size="sm" onClick={() => setWizardOpen(true)} disabled={sources.length === 0}>
          <Icon name="plus" size="xs" />
          New trigger
        </Button>
      </Stack>

      {loadError && <Alert variant="error">{loadError}</Alert>}
      {toggleError && <Alert variant="error">{toggleError}</Alert>}

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
            <Button size="sm" onClick={() => setWizardOpen(true)} disabled={sources.length === 0}>
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
              (row.event_type ?? '').toLowerCase().includes(q.toLowerCase())
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

      {wizardOpen && (
        <TriggerWizard
          agents={[{ id: agentOption.id, slug: agentOption.slug, name: agentOption.name }]}
          sources={sources.map((src) => ({
            id: src.id,
            name: src.name,
            provider: src.provider,
          }))}
          ingestHost={ingestHost}
          onCreate={createTriggerForAgent}
          onClose={() => {
            setWizardOpen(false);
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
