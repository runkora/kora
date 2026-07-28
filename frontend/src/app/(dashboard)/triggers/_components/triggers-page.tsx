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
  Input,
  SegmentedControl,
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
import { TriggerList } from './trigger-list';
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

/**
 * Where inbound webhooks arrive. Shown in the header because "what do I point
 * my provider at" is the first question a new webhook source raises.
 */
const INGEST_HOST_FALLBACK = 'this cluster';

export function TriggersPage({ sources, triggers, deliveries, agents, error }: TriggersPageProps) {
  const router = useRouter();
  const ingestHost = typeof window === 'undefined' ? INGEST_HOST_FALLBACK : window.location.host;

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
  const [query, setQuery] = useState('');

  // Kind filter only: DataTable owns the search box, so it also owns matching.
  const visibleTriggers = useMemo(() => {
    const q = query.trim().toLowerCase();
    return triggers.filter(
      (t) =>
        (kindFilter === 'all' || t.kind === kindFilter) &&
        (q === '' ||
          t.name.toLowerCase().includes(q) ||
          t.agentSlug.toLowerCase().includes(q) ||
          t.eventType.toLowerCase().includes(q)),
    );
  }, [triggers, kindFilter, query]);

  // Filter by how a trigger starts. Triggers are the front door for starting
  // work, so the kind is the primary way to slice them, and the counts are part
  // of the answer rather than decoration.
  const filterRow = (
    <Stack direction="horizontal" align="center" justify="between" gap="cozy" grow>
      <SegmentedControl
        aria-label="Filter triggers by kind"
        value={kindFilter}
        onChange={(v) => setKindFilter(v as TriggerKind | 'all')}
        options={[
          {
            value: 'all',
            label: 'All',
            icon: <Icon name="layers" size="2xs" />,
            count: triggers.length,
          },
          {
            value: 'webhook',
            label: 'Webhook',
            icon: <Icon name="webhook" size="2xs" />,
            count: triggers.filter((t) => t.kind === 'webhook').length,
          },
          {
            value: 'scheduled',
            label: 'Schedule',
            icon: <Icon name="clock" size="2xs" />,
            count: triggers.filter((t) => t.kind === 'scheduled').length,
          },
          {
            value: 'manual',
            label: 'Manual',
            icon: <Icon name="play" size="2xs" />,
            count: triggers.filter((t) => t.kind === 'manual').length,
          },
        ]}
      />
      <Stack grow maxWidth="search-field">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search triggers…"
          aria-label="Search triggers"
        />
      </Stack>
    </Stack>
  );

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

  /**
   * Persists a safety setting from the drawer.
   *
   * The drawer stays open: these are settings a reader adjusts while looking at
   * the trigger, not an action that finishes with it, so closing the panel
   * underneath them would lose their place.
   */
  const updateSafety = useCallback(
    async (row: TriggerRow, patch: Partial<TriggerRow>) => {
      await updateTrigger(row.id, {
        ...(patch.dedupeEnabled !== undefined && { dedupe_enabled: patch.dedupeEnabled }),
        ...(patch.autopauseEnabled !== undefined && { autopause_enabled: patch.autopauseEnabled }),
      });
      router.refresh();
    },
    [router],
  );

  // Read the open trigger back out of the live list rather than trusting the
  // row captured when the drawer opened: saving a setting inside the panel
  // refreshes the page data, and a snapshot would leave the control showing
  // the value it had before the save.
  const drawerRow =
    drawerTrigger === null
      ? null
      : (triggers.find((t) => t.id === drawerTrigger.id) ?? drawerTrigger);

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
        // align start: a column Stack stretches children by default, which
        // would pull the badge out to the column width.
        cell: (row) => (
          <Stack gap="none" align="start">
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
        <Stack direction="horizontal" justify="between" align="start">
          <Stack gap="tight" maxWidth="prose">
            <Heading variant="section">Triggers</Heading>
            <Text size="supporting" color="muted">
              Everything that starts an agent run: inbound webhooks, schedules and manual runs
              today, with control-plane events, chained runs and chat commands coming. Every
              delivery is recorded.
            </Text>
          </Stack>
          <Stack direction="horizontal" gap="snug" align="center">
            <Text size="caption" color="muted">
              Ingest
            </Text>
            <Text mono size="caption" color="muted">
              {ingestHost}
            </Text>
            <Button size="sm" onClick={() => setKindPickerOpen(true)}>
              <Icon name="plus" size="xs" />
              New trigger
            </Button>
          </Stack>
        </Stack>
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
            <TriggerList
              toolbar={filterRow}
              triggers={visibleTriggers}
              deliveries={deliveries}
              sourceName={sourceName}
              onOpen={setDrawerTrigger}
              onToggle={(t) => void toggleTrigger(t)}
              emptyMessage={
                triggers.length === 0
                  ? 'No triggers yet.'
                  : query.trim() !== ''
                    ? 'No triggers match this search.'
                    : `No ${kindFilter} triggers.`
              }
            />
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
        trigger={drawerRow}
        sources={sources}
        deliveries={deliveries}
        ingestHost={ingestHost}
        onClose={() => setDrawerTrigger(null)}
        onToggleEnabled={(t) => {
          setDrawerTrigger(null);
          void toggleTrigger(t);
        }}
        onDelete={(t) => {
          setDrawerTrigger(null);
          setDeleteTrg(t);
        }}
        onUpdateSafety={(t, patch) => void updateSafety(t, patch)}
        onFire={(t) => void fireManualTrigger(t)}
        firing={firingId !== null}
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
