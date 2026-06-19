'use client';

/** @tier 3 — triggers-scoped. Platform-wide Triggers control surface:
 * Sources / Triggers / Deliveries. */

import { useCallback, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Badge,
  Button,
  DataTable,
  Heading,
  Icon,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge, TriggerFormModal } from '@orbit/components';
import type { TriggerFormValue } from '@orbit/components';
import {
  deleteSource,
  deleteTrigger,
  updateSource,
  updateTrigger,
} from '../_services/triggers-api';
import { SourceFormModal } from './source-form-modal';
import { deliveryStatusTone } from '../_types';
import type { AgentOption, DeliveryRow, SourceRow, TriggerRow } from '../_types';

interface TriggersPageProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  agents: AgentOption[];
  error: string | null;
}

function formatTimestamp(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

export function TriggersPage({ sources, triggers, deliveries, agents, error }: TriggersPageProps) {
  const router = useRouter();

  const sourceName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sources) map.set(s.id, s.name);
    return (id: string) => map.get(id) ?? id;
  }, [sources]);

  const formSources = useMemo(() => sources.map((s) => ({ id: s.id, name: s.name })), [sources]);

  // ── Modal + dialog state ─────────────────────────────────────────────────────
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [triggerForm, setTriggerForm] = useState<{ trigger: TriggerFormValue | null } | null>(null);
  const [deleteSrc, setDeleteSrc] = useState<SourceRow | null>(null);
  const [deleteTrg, setDeleteTrg] = useState<TriggerRow | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const toggleSource = useCallback(
    async (row: SourceRow) => {
      await updateSource(row.id, { enabled: !row.enabled });
      router.refresh();
    },
    [router],
  );

  const toggleTrigger = useCallback(
    async (row: TriggerRow) => {
      await updateTrigger(row.id, { enabled: !row.enabled });
      router.refresh();
    },
    [router],
  );

  const confirmDeleteSource = useCallback(async () => {
    if (!deleteSrc) return;
    setDeleting(true);
    setDeleteError(null);
    const result = await deleteSource(deleteSrc.id);
    setDeleting(false);
    if (result.error) {
      setDeleteError(result.error);
      return;
    }
    setDeleteSrc(null);
    router.refresh();
  }, [deleteSrc, router]);

  const confirmDeleteTrigger = useCallback(async () => {
    if (!deleteTrg) return;
    setDeleting(true);
    setDeleteError(null);
    const result = await deleteTrigger(deleteTrg.id);
    setDeleting(false);
    if (result.error) {
      setDeleteError(result.error);
      return;
    }
    setDeleteTrg(null);
    router.refresh();
  }, [deleteTrg, router]);

  // ── Columns ──────────────────────────────────────────────────────────────────

  const sourceColumns = useMemo<DataTableColumn<SourceRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Text size="supporting" weight="emphasis">
            {row.name}
          </Text>
        ),
      },
      {
        id: 'provider',
        header: 'Provider',
        cell: (row) => (
          <Badge variant="outline" size="sm">
            {row.provider}
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
        width: '8rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" justify="end">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => void toggleSource(row)}
              aria-label={row.enabled ? `Disable ${row.name}` : `Enable ${row.name}`}
            >
              <Icon name={row.enabled ? 'eye-off' : 'eye'} size="2xs" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setDeleteSrc(row)}
              aria-label={`Delete ${row.name}`}
            >
              <Icon name="trash" size="2xs" />
            </Button>
          </Stack>
        ),
      },
    ],
    [toggleSource],
  );

  const triggerColumns = useMemo<DataTableColumn<TriggerRow>[]>(
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
              {sourceName(row.sourceId)}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'agent',
        header: 'Agent',
        cell: (row) => (
          <Text mono size="caption">
            {row.agentSlug}
          </Text>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        cell: (row) => (
          <Badge variant="elevated" size="sm">
            {row.eventType}
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
                setTriggerForm({
                  trigger: {
                    id: row.id,
                    name: row.name,
                    sourceId: row.sourceId,
                    agentTypeId: row.agentTypeId,
                    agentSlug: row.agentSlug,
                    eventType: row.eventType,
                    matchCondition: row.matchCondition,
                    promptTemplate: row.promptTemplate,
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
              onClick={() => setDeleteTrg(row)}
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

  const deliveryColumns = useMemo<DataTableColumn<DeliveryRow>[]>(
    () => [
      {
        id: 'received',
        header: 'Received',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.receivedAt,
        cell: (row) => (
          <Text mono size="caption">
            {formatTimestamp(row.receivedAt)}
          </Text>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        cell: (row) =>
          row.eventType ? (
            <Badge variant="outline" size="sm">
              {row.eventType}
            </Badge>
          ) : (
            <Text size="caption" color="muted">
              —
            </Text>
          ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: (row) => <StatusBadge status={deliveryStatusTone(row.status)} label={row.status} />,
      },
      {
        id: 'source',
        header: 'Source',
        cell: (row) => <Text size="caption">{sourceName(row.sourceId)}</Text>,
      },
      {
        id: 'detail',
        header: 'Detail',
        cell: (row) =>
          row.runs.length > 1 ? (
            <Text size="caption" color="muted">
              {row.runs.length} runs
            </Text>
          ) : row.error ? (
            <Text size="caption" color="muted">
              {row.error}
            </Text>
          ) : row.runId ? (
            <Text mono size="caption">
              run {row.runId.slice(0, 8)}
            </Text>
          ) : (
            <Text size="caption" color="muted">
              —
            </Text>
          ),
      },
    ],
    [sourceName],
  );

  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Heading variant="title">Triggers</Heading>
        <Text size="supporting" color="muted">
          Start agent runs automatically from external events. Register a webhook source, bind it to
          an agent with a trigger, and inspect deliveries. Webhook sources and triggers are managed
          by platform admins.
        </Text>
      </Stack>

      {error && <Alert variant="error">{error}</Alert>}

      <Tabs defaultValue="sources">
        <TabsList>
          <TabsTrigger value="sources">Sources</TabsTrigger>
          <TabsTrigger value="triggers">Triggers</TabsTrigger>
          <TabsTrigger value="deliveries">Deliveries</TabsTrigger>
        </TabsList>

        {/* Sources */}
        <TabsContent value="sources">
          <Stack gap="comfortable">
            <Stack direction="horizontal" justify="end">
              <Button size="sm" onClick={() => setSourceModalOpen(true)}>
                <Icon name="plus" size="xs" />
                New source
              </Button>
            </Stack>
            {sources.length === 0 ? (
              <EmptyState
                icon="zap"
                title="No webhook sources"
                description="Register a webhook source (e.g. a GitHub repository) to receive inbound events."
                action={
                  <Button size="sm" onClick={() => setSourceModalOpen(true)}>
                    <Icon name="plus" size="xs" />
                    New source
                  </Button>
                }
              />
            ) : (
              <DataTable
                data={sources}
                columns={sourceColumns}
                getRowKey={(row) => row.id}
                noun="sources"
                searchPlaceholder="Search sources…"
                searchFn={(row, q) => row.name.toLowerCase().includes(q.toLowerCase())}
                defaultSortId="name"
                defaultSortDir="asc"
                emptyMessage="No sources."
                filteredEmptyMessage="No sources match your search."
              />
            )}
          </Stack>
        </TabsContent>

        {/* Triggers */}
        <TabsContent value="triggers">
          <Stack gap="comfortable">
            <Stack direction="horizontal" justify="end">
              <Button
                size="sm"
                onClick={() => setTriggerForm({ trigger: null })}
                disabled={sources.length === 0}
              >
                <Icon name="plus" size="xs" />
                New trigger
              </Button>
            </Stack>
            {triggers.length === 0 ? (
              <EmptyState
                icon="zap"
                title="No triggers"
                description="Create a trigger to run an agent when a matching event arrives on a source."
                action={
                  <Button
                    size="sm"
                    onClick={() => setTriggerForm({ trigger: null })}
                    disabled={sources.length === 0}
                  >
                    <Icon name="plus" size="xs" />
                    New trigger
                  </Button>
                }
              />
            ) : (
              <DataTable
                data={triggers}
                columns={triggerColumns}
                getRowKey={(row) => row.id}
                noun="triggers"
                searchPlaceholder="Search triggers…"
                searchFn={(row, q) =>
                  row.name.toLowerCase().includes(q.toLowerCase()) ||
                  row.agentSlug.toLowerCase().includes(q.toLowerCase()) ||
                  row.eventType.toLowerCase().includes(q.toLowerCase())
                }
                defaultSortId="name"
                defaultSortDir="asc"
                emptyMessage="No triggers."
                filteredEmptyMessage="No triggers match your search."
              />
            )}
          </Stack>
        </TabsContent>

        {/* Deliveries */}
        <TabsContent value="deliveries">
          {deliveries.length === 0 ? (
            <EmptyState
              icon="activity"
              title="No deliveries yet"
              description="Inbound webhook deliveries and their outcomes appear here once a source receives events."
            />
          ) : (
            <DataTable
              data={deliveries}
              columns={deliveryColumns}
              getRowKey={(row) => row.id}
              noun="deliveries"
              searchPlaceholder="Search deliveries…"
              searchFn={(row, q) =>
                row.eventType.toLowerCase().includes(q.toLowerCase()) ||
                row.status.toLowerCase().includes(q.toLowerCase())
              }
              defaultSortId="received"
              defaultSortDir="desc"
              emptyMessage="No deliveries."
              filteredEmptyMessage="No deliveries match your search."
            />
          )}
        </TabsContent>
      </Tabs>

      {/* Modals */}
      <SourceFormModal
        open={sourceModalOpen}
        onClose={() => setSourceModalOpen(false)}
        onSaved={() => {
          setSourceModalOpen(false);
          router.refresh();
        }}
      />

      {triggerForm && (
        <TriggerFormModal
          key={triggerForm.trigger?.id ?? 'create'}
          open
          trigger={triggerForm.trigger}
          sources={formSources}
          agents={agents}
          onClose={() => setTriggerForm(null)}
          onSaved={() => {
            setTriggerForm(null);
            router.refresh();
          }}
        />
      )}

      {/* Delete confirmations */}
      <ConfirmDialog
        open={!!deleteSrc}
        title="Delete webhook source"
        description={`Permanently delete "${deleteSrc?.name ?? ''}"? Its triggers and signing secret are removed and its identity's grants revoked.`}
        confirmLabel="Delete source"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void confirmDeleteSource()}
        onClose={() => {
          setDeleteSrc(null);
          setDeleteError(null);
        }}
      />
      <ConfirmDialog
        open={!!deleteTrg}
        title="Delete trigger"
        description={`Permanently delete "${deleteTrg?.name ?? ''}"? It will stop firing runs.`}
        confirmLabel="Delete trigger"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void confirmDeleteTrigger()}
        onClose={() => {
          setDeleteTrg(null);
          setDeleteError(null);
        }}
      />
    </Stack>
  );
}
