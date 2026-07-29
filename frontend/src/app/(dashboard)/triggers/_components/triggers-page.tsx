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
  SidePanel,
  SidePanelBody,
  SidePanelHeader,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import type { DataTableColumn, TableColumnFilterOption } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import {
  deleteTrigger,
  fireTrigger,
  redeliverDelivery,
  updateSource,
  updateTrigger,
} from '../_services/triggers-api';
import { createFromWizardDraft } from '../_services/create-from-wizard';
import { DeliveryDetail } from './delivery-detail';
import { RunTriggerModal } from './run-trigger-modal';
import { TriggerWizard } from './trigger-wizard';
import type { TriggerCreateResult, TriggerDraft } from './trigger-wizard';
import { FleetKpis } from './fleet-kpis';
import { TriggerDrawer } from './trigger-drawer';
import type { TriggerEditPatch } from './trigger-drawer';
import { TriggerList } from './trigger-list';
import {
  deliveryDetail,
  deliveryDuration,
  deliveryStatusTone,
  deliveryTriggerLabel,
  matchesStatusFilter,
  toTriggerUpdateRequest,
} from '../_types';
import { ingestHostFromWindow } from '../_lib/endpoints';
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
  // Neither is a fault: one arrived twice, the other arrived while the platform
  // was not listening. Both are outcomes worth filtering to.
  { value: 'duplicate', label: 'Duplicate', dot: 'bg-status-idle' },
  { value: 'missed', label: 'Missed', dot: 'bg-status-idle' },
];

export function TriggersPage({ sources, triggers, deliveries, agents, error }: TriggersPageProps) {
  const router = useRouter();
  const ingestHost = ingestHostFromWindow();

  const sourceName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sources) map.set(s.id, s.name);
    return (id: string) => map.get(id) ?? id;
  }, [sources]);

  const formSources = useMemo(() => sources.map((s) => ({ id: s.id, name: s.name })), [sources]);

  // ── Modal + dialog state ─────────────────────────────────────────────────────
  const [wizardOpen, setWizardOpen] = useState(false);
  const [deleteTrg, setDeleteTrg] = useState<TriggerRow | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  // The platform Deliveries tab is not scoped to one trigger, so a row opens
  // its own panel rather than the owning trigger's drawer: the question being
  // asked is "what did this delivery do", not "how is that trigger set up".
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);
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
      <Stack direction="horizontal" gap="snug" align="center" grow justify="end">
        <Stack grow maxWidth="search-field">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search triggers…"
            aria-label="Search triggers"
          />
        </Stack>
        <Button size="sm" onClick={() => setWizardOpen(true)}>
          <Icon name="plus" size="xs" />
          New trigger
        </Button>
      </Stack>
    </Stack>
  );

  const [firingId, setFiringId] = useState<string | null>(null);
  /** Manual trigger whose input form is open. */
  const [runFormTrigger, setRunFormTrigger] = useState<TriggerRow | null>(null);
  const [fireResult, setFireResult] = useState<{ ok: boolean; message: string } | null>(null);

  const fireManualTrigger = useCallback(
    async (row: TriggerRow, variables?: Record<string, string>): Promise<string | null> => {
      setFiringId(row.id);
      setFireResult(null);
      const result = await fireTrigger(row.id, variables);
      setFiringId(null);
      if (result.error !== null) {
        setFireResult({ ok: false, message: `${row.name}: ${result.error}` });
        return result.error;
      }
      // The fire is durably queued and the run starts moments later, so there
      // is no run to name yet. Reporting one here would mean starting the run
      // before acknowledging, which is what loses fires on a restart.
      setFireResult({ ok: true, message: `${row.name}: queued. The run starts shortly.` });
      // The delivery exists now; refresh so the drawer's Deliveries tab shows it.
      router.refresh();
      return null;
    },
    [router],
  );

  /**
   * Running a manual trigger goes through its declared form when it has one;
   * a trigger with no inputs fires directly. The same declaration validates
   * the API call, so the form cannot drift from what /fire accepts.
   */
  const startManualRun = useCallback(
    (row: TriggerRow) => {
      if ((row.manualInputs ?? []).length > 0) {
        setRunFormTrigger(row);
        return;
      }
      void fireManualTrigger(row);
    },
    [fireManualTrigger],
  );

  /**
   * Executes the wizard's draft: the source first when one was asked for, then
   * the trigger. The secret only exists when a source was just created, and it
   * is passed straight through to the done screen; nothing stores it.
   */
  const createFromWizard = useCallback(
    async (draft: TriggerDraft): Promise<TriggerCreateResult> => {
      const result = await createFromWizardDraft(draft, { ingestHost });
      if (result.ok) router.refresh();
      return result;
    },
    [router, ingestHost],
  );

  /** Rotates a webhook trigger's source secret; the drawer shows it once. */
  const rotateSecret = useCallback(async (row: TriggerRow): Promise<string | null> => {
    const result = await updateSource(row.sourceId, { rotate_secret: true });
    if (result.error || !result.source?.secret) {
      setFireResult({
        ok: false,
        message: `${row.name}: ${result.error ?? 'rotation returned no secret'}`,
      });
      return null;
    }
    return result.source.secret;
  }, []);

  const toggleTrigger = useCallback(
    async (row: TriggerRow) => {
      const result = await updateTrigger(row.id, toTriggerUpdateRequest({ enabled: !row.enabled }));
      if (result.error) {
        setFireResult({ ok: false, message: `${row.name}: ${result.error}` });
        return;
      }
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
      const result = await updateTrigger(row.id, toTriggerUpdateRequest(patch));
      if (result.error) {
        setFireResult({ ok: false, message: `${row.name}: ${result.error}` });
        return;
      }
      router.refresh();
    },
    [router],
  );

  /**
   * Persists a correction made in the drawer.
   *
   * Unlike the safety controls, this resolves with the failure rather than
   * raising a page-level notice: the drawer keeps the edit on screen next to
   * the reason, so nothing typed is lost to a rejected save.
   */
  const saveTriggerEdit = useCallback(
    async (row: TriggerRow, patch: TriggerEditPatch): Promise<string | null> => {
      const result = await updateTrigger(row.id, toTriggerUpdateRequest(patch));
      if (result.error) return result.error;
      router.refresh();
      return null;
    },
    [router],
  );

  /**
   * Replays a delivery.
   *
   * The refresh follows the call so the new arrival shows up in the list; the
   * result is handed back to the caller, which reports it beside the delivery
   * rather than as a page-level notice.
   */
  const redeliver = useCallback(
    async (row: DeliveryRow): Promise<{ deliveryId: string | null; error: string | null }> => {
      const result = await redeliverDelivery(row.id);
      if (result.error === null) router.refresh();
      return result;
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
        id: 'trigger',
        header: 'Trigger',
        cell: (row) => {
          const label = deliveryTriggerLabel(row);
          return (
            <Text size="caption" color={label === '—' ? 'muted' : undefined} truncate>
              {label}
            </Text>
          );
        },
      },
      {
        id: 'duration',
        header: 'Duration',
        width: '7rem',
        sortable: true,
        // Sorted on the raw gap, not the formatted string, so "900ms" cannot
        // sort above "1.2s".
        sortAccessor: (row) => {
          const started = row.runs[0]?.createdAt;
          return started === undefined || started === null
            ? -1
            : Date.parse(started) - Date.parse(row.receivedAt);
        },
        cell: (row) => (
          <Text mono size="caption" color="muted">
            {deliveryDuration(row)}
          </Text>
        ),
      },
      {
        id: 'detail',
        header: 'Detail',
        width: '14rem',
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
              Everything that starts work on the platform: inbound webhooks, schedules and manual
              fires today, with control-plane events, chained runs and chat commands coming. Every
              delivery is recorded.
            </Text>
          </Stack>
        </Stack>
      </Stack>

      {error && <Alert variant="error">{error}</Alert>}
      {fireResult && <Alert variant={fireResult.ok ? 'info' : 'error'}>{fireResult.message}</Alert>}

      <FleetKpis triggers={triggers} deliveries={deliveries} />

      {/* Sources are not a headline surface: a webhook source is created
          inside the New Trigger flow and managed from its trigger's drawer,
          so the page stays about what starts work. */}
      <Tabs defaultValue="triggers">
        <TabsList>
          <TabsTrigger value="triggers">Triggers</TabsTrigger>
          <TabsTrigger value="deliveries">Deliveries</TabsTrigger>
        </TabsList>

        <TabsContent value="triggers">
          <Stack gap="comfortable">
            <TriggerList
              toolbar={filterRow}
              resetKey={`${kindFilter}|${query.trim()}`}
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
              onRowClick={setDetailDelivery}
              onRowClickLabel="View payload and outcome"
            />
          )}
        </TabsContent>
      </Tabs>

      {/* One delivery's payload and outcome, drawer-free: the row was clicked
          to ask what this arrival did, not to configure anything. */}
      {detailDelivery !== null && (
        <SidePanel open onClose={() => setDetailDelivery(null)} size="md">
          <SidePanelHeader
            density="compact"
            title={detailDelivery.eventType || detailDelivery.kind}
            description={`Delivery ${detailDelivery.id}`}
          />
          <SidePanelBody padding="compact">
            <Stack gap="comfortable">
              <Stack direction="horizontal" align="center" gap="cozy">
                <StatusBadge
                  status={deliveryStatusTone(detailDelivery.status)}
                  label={detailDelivery.status}
                />
                <Text size="caption" color="muted">
                  {formatTimestamp(detailDelivery.receivedAt)}
                </Text>
              </Stack>
              <DeliveryDetail delivery={detailDelivery} onRedeliver={redeliver} />
              {detailDelivery.triggerId !== null && (
                <Stack direction="horizontal" justify="end">
                  <Button
                    variant="ghost"
                    size="xs"
                    onClick={() => {
                      const owner = triggers.find((t) => t.id === detailDelivery.triggerId);
                      setDetailDelivery(null);
                      if (owner !== undefined) setDrawerTrigger(owner);
                    }}
                  >
                    Open trigger
                    <Icon name="arrow-right" size="2xs" />
                  </Button>
                </Stack>
              )}
            </Stack>
          </SidePanelBody>
        </SidePanel>
      )}

      {/* Modals */}
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
        onEdit={saveTriggerEdit}
        onRedeliver={redeliver}
        onRotateSecret={rotateSecret}
        onFire={startManualRun}
        // Scoped to the trigger on screen: firing one trigger must not make
        // another one's panel claim it is queueing.
        firing={firingId === drawerRow?.id}
      />

      {/* One modal owns the whole creation flow: kind picker, the kind's own
          steps from the registry, the shared steps, and the success screen. */}
      {wizardOpen && (
        <TriggerWizard
          agents={agents}
          sources={formSources.map((f) => ({
            ...f,
            provider: sources.find((x) => x.id === f.id)?.provider ?? 'github',
          }))}
          ingestHost={ingestHost}
          onCreate={createFromWizard}
          onClose={() => setWizardOpen(false)}
        />
      )}

      {runFormTrigger !== null && (
        <RunTriggerModal
          trigger={runFormTrigger}
          onClose={() => setRunFormTrigger(null)}
          onFire={(variables) => fireManualTrigger(runFormTrigger, variables)}
        />
      )}

      {/* Delete confirmations */}
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
