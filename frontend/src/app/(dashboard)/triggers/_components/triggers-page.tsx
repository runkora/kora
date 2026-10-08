'use client';

/** @tier 3 — triggers-scoped. Platform-wide Triggers control surface:
 * Sources / Triggers / Deliveries. */

import { useCallback, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  NativeSelect,
  SegmentedControl,
  Heading,
  Icon,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
  ConfirmDialog,
} from '@orbit/hades';
import { createFromWizardDraft } from '../_services/create-from-wizard';
import { useTriggerActions } from './use-trigger-actions';
import type { TriggerNotice } from './use-trigger-actions';
import { DeliveryDetailPanel } from './delivery-detail-panel';
import { RunTriggerModal } from './run-trigger-modal';
import { TriggerWizard } from './trigger-wizard';
import type { TriggerCreateResult, TriggerDraft } from './trigger-wizard';
import { FleetKpis } from './fleet-kpis';
import { TriggerDrawer } from './trigger-drawer';
import { TriggerList } from './trigger-list';
import { DeliveryGrid } from './delivery-grid';
import { toDeliveryRow, DELIVERY_PAGE_SIZE } from '../_types';
import { listDeliveries } from '../_services/triggers-api';
import { ingestHostFromWindow } from '../_lib/endpoints';
import type { AgentOption, DeliveryRow, SourceRow, TriggerKind, TriggerRow } from '../_types';

interface TriggersPageProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  /**
   * Cursor for the page after `deliveries`, or null when the first page is all
   * there is. Comes from the same server fetch, so the first Load more does not
   * have to re-read page one to discover where it ended.
   */
  initialDeliveryCursor?: string | null;
  agents: AgentOption[];
  error: string | null;
}

const DELIVERY_STATUS_OPTIONS = [
  { value: 'dispatched', label: 'Dispatched' },
  { value: 'verified', label: 'Verified' },
  { value: 'skipped', label: 'Skipped' },
  { value: 'failed', label: 'Failed' },
  // Neither is a fault: one arrived twice, the other arrived while the platform
  // was not listening. Both are outcomes worth filtering to.
  { value: 'duplicate', label: 'Duplicate' },
  { value: 'missed', label: 'Missed' },
];

/**
 * The deliveries list as one value: the rows on screen, the cursor that
 * continues them, the filter they actually are, and the epoch that says which
 * server page they belong to. They change together, so a response is accepted
 * or rejected as a unit rather than leaving the four half-updated.
 */
interface DeliveryPageState {
  rows: DeliveryRow[];
  cursor: string | null;
  applied: string;
  epoch: number;
  /**
   * error and loading live here too, not in their own useState. A request the
   * re-seed retired would otherwise still publish its error onto the fresh
   * page and hold Load more disabled until it returned, which is the opposite
   * of accepting or rejecting a response as a unit.
   */
  error: string | null;
  loading: boolean;
}

export function TriggersPage({
  sources,
  triggers,
  deliveries,
  initialDeliveryCursor = null,
  agents,
  error,
}: TriggersPageProps) {
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
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  // The platform Deliveries tab is not scoped to one trigger, so a row opens
  // its own panel rather than the owning trigger's drawer: the question being
  // asked is "what did this delivery do", not "how is that trigger set up".
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);
  const [kindFilter, setKindFilter] = useState<TriggerKind | 'all'>('all');

  // Filter by how a trigger starts. Triggers are the front door for starting
  // work, so the kind is the primary way to slice them, and the counts are part
  // of the answer rather than decoration.
  const visibleTriggers = useMemo(
    () => (kindFilter === 'all' ? triggers : triggers.filter((t) => t.kind === kindFilter)),
    [triggers, kindFilter],
  );

  // ── Deliveries: status filter and paging ──────────────────────────────────────
  //
  // The filter and the page both belong to the server now: filtering in the
  // browser could only ever narrow the rows already loaded, so "failed" meant
  // "failed among the most recent page", and older failures were unreachable.
  // The grid keeps its own page controls; this list is the set they page over,
  // and Load more extends it.
  //
  // selectedStatus is what the control shows; page.applied is what the rows on
  // screen actually are. They differ while a request is in flight, and keeping
  // them apart stops the grid relabelling the previous filter's rows as the new
  // filter's answer.
  const [selectedStatus, setSelectedStatus] = useState('all');
  const [page, setPage] = useState<DeliveryPageState>({
    rows: deliveries,
    cursor: initialDeliveryCursor,
    applied: 'all',
    epoch: 0,
    error: null,
    loading: false,
  });

  // Only the newest request may write. Without this, changing the filter while a
  // Load more is in flight lets the older response append the previous filter's
  // rows and overwrite the new filter's cursor, after which every further page
  // walks the wrong keyset.
  const requestSeq = useRef(0);

  // rows, cursor and applied move together, in one state object with an epoch,
  // because a response has to be accepted or rejected as a unit. The epoch is
  // what the render-phase re-seed below bumps, and comparing it inside the
  // updater is the only place the CURRENT value is readable: a ref cannot be
  // written during render and a closure captured at request time is stale by
  // definition. Without it a Load more that overlaps a refresh appends the old
  // filter's rows onto the new server page and installs the old keyset's
  // cursor, which leaves the deliveries in between permanently unreachable.
  const fetchDeliveries = useCallback(
    async (status: string, cursor: string | null, epoch: number) => {
      const seq = ++requestSeq.current;
      setPage((cur) => (cur.epoch !== epoch ? cur : { ...cur, error: null, loading: true }));
      const res = await listDeliveries({
        status: status === 'all' ? undefined : [status],
        limit: DELIVERY_PAGE_SIZE,
        cursor,
      });
      if (requestSeq.current !== seq) return; // superseded; its writes are stale
      setPage((cur) => {
        // A fresh server page landed while this was in flight; it owns the
        // list, the error line and the loading state alike.
        if (cur.epoch !== epoch) return cur;
        if (res.error) {
          // Clear the page rather than leave the previous filter's rows sitting
          // under the new label. An empty list with an error says "could not
          // load"; stale rows would claim to be the answer. A failed Load more
          // keeps its rows and cursor, because the page it has is still true.
          return cursor === null
            ? { ...cur, loading: false, error: res.error, rows: [], cursor: null, applied: status }
            : { ...cur, loading: false, error: res.error };
        }
        const rows = res.deliveries.map(toDeliveryRow);
        return {
          ...cur,
          loading: false,
          error: null,
          rows: cursor === null ? rows : [...cur.rows, ...rows],
          cursor: res.nextCursor,
          applied: status,
        };
      });
    },
    [],
  );

  // A new filter is a new query, not a narrowing of what is on screen, so it
  // reads from the start rather than from the current cursor.
  const applyDeliveryStatus = useCallback(
    (next: string) => {
      setSelectedStatus(next);
      void fetchDeliveries(next, null, page.epoch);
    },
    [fetchDeliveries, page.epoch],
  );

  const loadMoreDeliveries = useCallback(() => {
    if (page.cursor === null || page.loading) return;
    void fetchDeliveries(page.applied, page.cursor, page.epoch);
  }, [page.applied, page.cursor, page.epoch, page.loading, fetchDeliveries]);

  // A failed filter change leaves nothing to click: the rows and cursor are
  // gone, so there is no Load more, and re-picking the same option fires no
  // change event. Retry is the way back.
  const retryDeliveries = useCallback(() => {
    if (page.loading) return;
    void fetchDeliveries(selectedStatus, null, page.epoch);
  }, [selectedStatus, page.epoch, page.loading, fetchDeliveries]);

  // The server re-renders this page after a fire, redeliver or delete
  // (use-trigger-actions calls router.refresh so the new arrival shows up). A
  // prop read once at mount would never see it, so the accumulated list is
  // re-seeded whenever a new server page arrives.
  //
  // This is React's "adjust state when a prop changes" pattern, done during
  // render rather than in an effect: an effect would paint the stale list first
  // and is what the repo's no-setState-in-effect rule exists to prevent.
  //
  // Re-seeding also drops back to All. The alternative, re-running the active
  // filter, means a second round trip before the row the user just created can
  // appear, and they refreshed precisely to see it.
  const [serverPage, setServerPage] = useState(deliveries);
  if (serverPage !== deliveries) {
    setServerPage(deliveries);
    // Bumping the epoch retires any request still in flight: its updater sees a
    // different epoch and returns the state untouched, so the fresh server page
    // keeps the list rather than having the old filter's rows appended to it.
    setPage((cur) => ({
      rows: deliveries,
      cursor: initialDeliveryCursor,
      applied: 'all',
      epoch: cur.epoch + 1,
      error: null,
      loading: false,
    }));
    setSelectedStatus('all');
  }

  // A delivery's fan-out names the trigger; the run route is keyed by agent
  // slug. The page already has every trigger, so the lookup is local.
  const agentSlugForTrigger = useCallback(
    (id: string) => triggers.find((t) => t.id === id)?.agentSlug,
    [triggers],
  );

  const [fireResult, setFireResult] = useState<TriggerNotice | null>(null);

  // Every mutation this page can perform, shared with the agent page's
  // Triggers tab so the two surfaces drive the same drawer identically.
  const refresh = useCallback(() => router.refresh(), [router]);
  const {
    fireManualTrigger,
    startManualRun,
    rotateSecret,
    toggleTrigger,
    updateSafety,
    saveTriggerEdit,
    redeliver,
    firingId,
    runFormTrigger,
    setRunFormTrigger,
    deleteCandidate,
    setDeleteCandidate,
    deleting,
    deleteError,
    confirmDelete,
    closeDeleteDialog,
  } = useTriggerActions({ refresh, notify: setFireResult });

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

  // Read the open trigger back out of the live list rather than trusting the
  // row captured when the drawer opened: saving a setting inside the panel
  // refreshes the page data, and a snapshot would leave the control showing
  // the value it had before the save.
  const drawerRow =
    drawerTrigger === null
      ? null
      : (triggers.find((t) => t.id === drawerTrigger.id) ?? drawerTrigger);

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
              quickFilters={
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
              }
              toolbarActions={
                <Button size="sm" onClick={() => setWizardOpen(true)}>
                  <Icon name="plus" size="xs" />
                  New trigger
                </Button>
              }
              resetKey={kindFilter}
              triggers={visibleTriggers}
              deliveries={deliveries}
              sourceName={sourceName}
              onOpen={setDrawerTrigger}
              onToggle={(t) => void toggleTrigger(t)}
              emptyMessage={
                triggers.length === 0
                  ? 'No triggers yet.'
                  : `No ${kindFilter === 'all' ? '' : kindFilter + ' '}triggers.`
              }
            />
          </Stack>
        </TabsContent>

        {/* Deliveries */}
        <TabsContent value="deliveries">
          <DeliveryGrid
            quickFilters={
              <NativeSelect
                aria-label="Filter deliveries by status"
                value={selectedStatus}
                onChange={(e) => applyDeliveryStatus(e.target.value)}
              >
                <option value="all">All statuses</option>
                {DELIVERY_STATUS_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </NativeSelect>
            }
            resetKey={page.applied}
            deliveries={page.rows}
            sourceName={sourceName}
            onOpen={setDetailDelivery}
            emptyMessage={
              page.applied === 'all'
                ? 'No deliveries yet. Arrivals and their outcomes appear here once a source receives events, a schedule ticks, or a trigger is fired.'
                : `No ${page.applied} deliveries.`
            }
          />
          {/* Below the grid, because it extends the set the grid pages over
              rather than moving within it. Absent once the cursor is spent,
              which is how "that is all of them" is said. */}
          {(page.cursor !== null || page.error !== null) && (
            <Stack direction="horizontal" align="center" gap="snug">
              {page.cursor !== null && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page.loading}
                  onClick={loadMoreDeliveries}
                >
                  {page.loading ? 'Loading...' : 'Load more'}
                </Button>
              )}
              {page.error !== null && (
                <>
                  <Text size="supporting" color="status-error">
                    {page.error}
                  </Text>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page.loading}
                    onClick={retryDeliveries}
                  >
                    Retry
                  </Button>
                </>
              )}
            </Stack>
          )}
        </TabsContent>
      </Tabs>

      {/* One delivery's payload and outcome. Opening the trigger swaps the
          panel for the owning trigger's drawer. */}
      {detailDelivery !== null && (
        <DeliveryDetailPanel
          delivery={detailDelivery}
          onClose={() => setDetailDelivery(null)}
          onRedeliver={redeliver}
          resolveAgentSlug={agentSlugForTrigger}
          onOpenTrigger={(triggerId) => {
            const owner = triggers.find((t) => t.id === triggerId);
            setDetailDelivery(null);
            if (owner !== undefined) setDrawerTrigger(owner);
          }}
        />
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
          setDeleteCandidate(t);
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
        open={!!deleteCandidate}
        title="Delete trigger"
        description={`Permanently delete "${deleteCandidate?.name ?? ''}"? It will stop firing runs.`}
        confirmLabel="Delete trigger"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void confirmDelete()}
        onClose={closeDeleteDialog}
      />
    </Stack>
  );
}
