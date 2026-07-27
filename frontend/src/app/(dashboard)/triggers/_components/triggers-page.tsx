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
  SidePanel,
  SidePanelBody,
  SidePanelHeader,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import type { DataTableColumn, TableColumnFilterOption } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge, TriggerFormModal } from '@orbit/components';
import type { TriggerFormValue } from '@orbit/components';
import {
  deleteSource,
  deleteTrigger,
  fireTrigger,
  updateSource,
  updateTrigger,
} from '../_services/triggers-api';
import { SourceFormModal } from './source-form-modal';
import { FleetKpis } from './fleet-kpis';
import { TriggerDrawer } from './trigger-drawer';
import { TriggerKindPicker } from './trigger-kind-picker';
import { TriggerSparkline } from './trigger-sparkline';
import { lastFired, needsAttention, recentFailures, sparkline } from '../_types/fleet';
import { deliveryDetail, deliveryStatusTone, matchesStatusFilter } from '../_types';
import type { AgentOption, DeliveryRow, SourceRow, TriggerKind, TriggerRow } from '../_types';

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

const DELIVERY_STATUS_OPTIONS: TableColumnFilterOption[] = [
  { value: 'dispatched', label: 'Dispatched', dot: 'bg-status-healthy' },
  { value: 'verified', label: 'Verified', dot: 'bg-status-degraded' },
  { value: 'skipped', label: 'Skipped', dot: 'bg-status-degraded' },
  { value: 'failed', label: 'Failed', dot: 'bg-status-error' },
];

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
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  const [kindFilter, setKindFilter] = useState<TriggerKind | 'all'>('all');
  const [kindPickerOpen, setKindPickerOpen] = useState(false);
  const [firingId, setFiringId] = useState<string | null>(null);
  const [fireResult, setFireResult] = useState<{ ok: boolean; message: string } | null>(null);

  const fireManualTrigger = useCallback(async (row: TriggerRow) => {
    setFiringId(row.id);
    setFireResult(null);
    const result = await fireTrigger(row.id);
    setFiringId(null);
    if ('error' in result && result.error) {
      setFireResult({ ok: false, message: `${row.name}: ${result.error}` });
      return;
    }
    // The fire is durably queued and the run starts moments later, so there is
    // no run to name yet. Reporting one here would mean starting the run before
    // acknowledging, which is what loses fires on a restart.
    setFireResult({ ok: true, message: `${row.name}: queued. The run starts shortly.` });
  }, []);

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
          <Stack gap="none">
            <StatusBadge
              status={row.enabled ? 'healthy' : 'unknown'}
              label={row.enabled ? 'Enabled' : 'Disabled'}
            />
            {/* Requests that failed authentication never become deliveries, so
                this counter is the only place a wrong signing secret shows up. */}
            {row.rejectionCount > 0 && (
              <Text size="caption" color="muted">
                {row.rejectionCount} rejected
                {row.lastRejectionReason !== null ? `: ${row.lastRejectionReason}` : ''}
              </Text>
            )}
          </Stack>
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
        cell: (row) => {
          const failures = recentFailures(row.id, deliveries);
          return (
            <Stack gap="none" align="start">
              <Stack direction="horizontal" gap="snug" align="center">
                {/* The name opens the drawer: everything about one trigger is
                    in one place, rather than spread across three tabs. */}
                <Button variant="ghost" size="sm" onClick={() => setDrawerTrigger(row)}>
                  {row.name}
                </Button>
                {/* Derived from this trigger's own recent outcomes, never a
                    stored flag that could drift from the deliveries below. */}
                {failures > 0 && (
                  <Badge variant="outline" size="sm">
                    {failures} failed
                  </Badge>
                )}
              </Stack>
              <Text size="caption" color="muted">
                {row.kind === 'webhook'
                  ? sourceName(row.sourceId)
                  : row.kind === 'scheduled'
                    ? (row.scheduleConfig?.cronExpression ?? 'schedule')
                    : 'manual'}
              </Text>
            </Stack>
          );
        },
      },
      {
        id: 'activity',
        header: '24h',
        width: '7rem',
        cell: (row) => (
          <TriggerSparkline buckets={sparkline(row.id, deliveries)} label={row.name} />
        ),
      },
      {
        id: 'lastFired',
        header: 'Last fired',
        cell: (row) => {
          const at = lastFired(row.id, deliveries);
          return (
            <Text size="caption" color="muted">
              {at === null ? 'Never' : new Date(at).toLocaleString()}
            </Text>
          );
        },
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
        cell: (row) =>
          row.eventType ? (
            <Badge variant="elevated" size="sm">
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
                    kind: row.kind,
                    name: row.name,
                    sourceId: row.sourceId,
                    agentTypeId: row.agentTypeId,
                    agentSlug: row.agentSlug,
                    eventType: row.eventType,
                    matchCondition: row.matchCondition,
                    promptTemplate: row.promptTemplate,
                    scheduleConfig: row.scheduleConfig,
                  },
                })
              }
              aria-label={`Edit ${row.name}`}
            >
              <Icon name="edit" size="2xs" />
            </Button>
            {row.kind === 'manual' && (
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!row.enabled || firingId === row.id}
                onClick={() => void fireManualTrigger(row)}
                aria-label={`Fire ${row.name}`}
              >
                <Icon name="play" size="2xs" />
              </Button>
            )}
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
    [sourceName, toggleTrigger, fireManualTrigger, firingId, deliveries],
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
        filter: {
          mode: 'client',
          options: DELIVERY_STATUS_OPTIONS,
          filterFn: matchesStatusFilter,
        },
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
        cell: (row) => {
          const detail = deliveryDetail(row);
          switch (detail.kind) {
            case 'runs':
            case 'error':
              return (
                <Text size="caption" color="muted">
                  {detail.text}
                </Text>
              );
            case 'runId':
              return (
                <Text mono size="caption">
                  {detail.text}
                </Text>
              );
            case 'excerpt':
              return (
                <Text mono size="caption" color="muted">
                  {detail.text}
                </Text>
              );
            case 'empty':
              return (
                <Text size="caption" color="muted">
                  —
                </Text>
              );
          }
        },
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
      {fireResult && <Alert variant={fireResult.ok ? 'info' : 'error'}>{fireResult.message}</Alert>}

      <FleetKpis triggers={triggers} deliveries={deliveries} />

      {/* Triggers first: the page is about what starts work. Sources are the
          configuration behind one kind of trigger, not the headline. */}
      <Tabs defaultValue="triggers">
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
            <Stack direction="horizontal" justify="between" align="center">
              {/* Filter by how a trigger starts. Triggers are the front door for
                  starting work, so the kind is the primary way to slice them. */}
              <Stack direction="horizontal" gap="tight">
                {(['all', 'webhook', 'scheduled', 'manual'] as const).map((k) => (
                  <Button
                    key={k}
                    size="sm"
                    variant={kindFilter === k ? 'secondary' : 'ghost'}
                    onClick={() => setKindFilter(k)}
                  >
                    {k === 'all' ? 'All' : k}
                    {k !== 'all' && ` (${triggers.filter((t) => t.kind === k).length})`}
                  </Button>
                ))}
              </Stack>
              <Button size="sm" onClick={() => setKindPickerOpen(true)}>
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
                data={
                  kindFilter === 'all' ? triggers : triggers.filter((t) => t.kind === kindFilter)
                }
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
              filteredEmptyMessage="No deliveries match your search or filter."
            />
          )}
        </TabsContent>
      </Tabs>

      {/* Modals */}
      {/* Choosing how a trigger starts is the first decision, so it gets its own
          step rather than a dropdown buried in the form. It also shows the kinds
          the model accommodates but that are not built yet. */}
      {kindPickerOpen && (
        <SidePanel open onClose={() => setKindPickerOpen(false)} size="lg">
          <SidePanelHeader
            title="New trigger"
            description="Choose how this trigger starts. What it starts is chosen next."
          />
          <SidePanelBody>
            <TriggerKindPicker
              onSelect={(kind) => {
                setKindPickerOpen(false);
                setTriggerForm({ trigger: { kind } as TriggerFormValue });
              }}
            />
          </SidePanelBody>
        </SidePanel>
      )}

      <TriggerDrawer
        trigger={drawerTrigger}
        sources={sources}
        deliveries={deliveries}
        onClose={() => setDrawerTrigger(null)}
        onToggleEnabled={(t) => {
          setDrawerTrigger(null);
          void toggleTrigger(t);
        }}
        onDelete={(t) => {
          setDrawerTrigger(null);
          setDeleteTrg(t);
        }}
      />

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
