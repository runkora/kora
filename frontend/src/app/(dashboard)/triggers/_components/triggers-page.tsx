'use client';

/** @tier 3 — triggers-scoped. Platform-wide Triggers control surface:
 * Sources / Triggers / Deliveries. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
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
} from '@runkora/hades';
import { createFromWizardDraft } from '../_services/create-from-wizard';
import { useTriggerActions } from './use-trigger-actions';
import type { TriggerNotice } from './use-trigger-actions';
import { DeliveryDetailPanel, runHrefFor } from './delivery-detail-panel';
import { RunTriggerModal } from './run-trigger-modal';
import { TriggerWizard } from './trigger-wizard';
import type { TriggerCreateResult, TriggerDraft } from './trigger-wizard';
import { TriggerStatsKpis } from './trigger-stats-kpis';
import { TriggerDetailModal } from './trigger-detail-modal';
import { TriggerList } from './trigger-list';
import { DeliveryGrid } from './delivery-grid';
import { consoleKindOf, runTargets } from '../_types';
import { STATS_RANGE_OPTIONS, rangeSuffix, statsWindowOf } from '../_types/stats';
import type { StatsRange, TenantStatsRow } from '../_types/stats';
import { useTriggerDeliveries } from './use-trigger-deliveries';
import { useStatsRange } from './use-stats-range';
import { ingestHostFromWindow } from '../_lib/endpoints';
import type {
  AgentOption,
  ConsoleKind,
  DeliveryRow,
  SourceCategory,
  SourceRow,
  TriggerRow,
} from '../_types';
import { useHasScope, useTenantLocked } from '../../_components/permissions-provider';
import { ManagedInGitCallout, PermissionHint } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';

interface TriggersPageProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  /** Null when the API answered without tenant stats. */
  stats: TenantStatsRow | null;
  /** The `?window=` range the server loaded the stats for. */
  range: StatsRange;
  agents: AgentOption[];
  agentsError?: string | null;
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

/** How far back a linked delivery is looked for before the tab gives up on opening it. */
const LINKED_DELIVERY_SEARCH = 250;

/** The kind in the empty-list message, e.g. "No chat command triggers." */
const KIND_FILTER_NOUN: Record<ConsoleKind, string> = {
  webhook: 'webhook',
  chat: 'chat command',
  scheduled: 'scheduled',
  manual: 'manual',
  event: 'event',
};

export function TriggersPage({
  sources,
  triggers,
  stats,
  range,
  agents,
  agentsError = null,
  error,
}: TriggersPageProps) {
  const router = useRouter();
  // One range for the cards, the list's activity column, the Deliveries tab and the open modal.
  const { setRange } = useStatsRange();
  // `?trigger=<id>` opens a trigger's modal, and `?tab=deliveries&delivery=<id>` a delivery's.
  const searchParams = useSearchParams();
  const [initialTab] = useState(() =>
    searchParams.get('tab') === 'deliveries' || searchParams.has('delivery')
      ? 'deliveries'
      : 'triggers',
  );
  const [linkedDelivery, setLinkedDelivery] = useState(() => searchParams.get('delivery'));
  const canWrite = useHasScope('triggers:write');
  // A locked tenant takes its triggers and sources from the configuration repository.
  const locked = useTenantLocked();
  const ingestHost = ingestHostFromWindow();

  const canRunAgent = useCallback(
    (agentTypeId: string) => agents.some((a) => a.id === agentTypeId && a.canRun),
    [agents],
  );
  const canRunTrigger = (t: TriggerRow | undefined) =>
    t !== undefined && runTargets(t).every(canRunAgent);
  const canRunOwner = (triggerId: string | null) =>
    canRunTrigger(triggers.find((t) => t.id === triggerId));

  const sourceName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sources) map.set(s.id, s.name);
    return (id: string) => map.get(id) ?? id;
  }, [sources]);

  const sourceCategory = useMemo(() => {
    const map = new Map<string, SourceCategory>();
    for (const s of sources) map.set(s.id, s.category);
    return (id: string) => map.get(id);
  }, [sources]);

  const sourceWaiting = useMemo(() => {
    const waiting = new Set(sources.filter((s) => !s.signingSecretSet).map((s) => s.id));
    return (id: string) => waiting.has(id);
  }, [sources]);

  const wizardSources = useMemo(
    () =>
      sources.map((s) => ({
        id: s.id,
        name: s.name,
        provider: s.provider,
        category: s.category,
        signingSecretSet: s.signingSecretSet,
        config: s.config,
      })),
    [sources],
  );

  // ── Modal + dialog state ─────────────────────────────────────────────────────
  const [wizardOpen, setWizardOpen] = useState(false);
  const [modalTrigger, setModalTrigger] = useState<TriggerRow | null>(
    () => triggers.find((t) => t.id === searchParams.get('trigger')) ?? null,
  );
  // The platform Deliveries tab is not scoped to one trigger, so a row opens
  // its own panel rather than the owning trigger's modal: the question being
  // asked is "what did this delivery do", not "how is that trigger set up".
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);
  const [kindFilter, setKindFilter] = useState<ConsoleKind | 'all'>('all');

  // Filter by how a trigger starts. Triggers are the front door for starting
  // work, so the kind is the primary way to slice them, and the counts are part
  // of the answer rather than decoration.
  // A chat command is a webhook trigger on a chat source, so it is counted
  // under Chat command and never under Webhook.
  const kindCount = (kind: ConsoleKind) =>
    triggers.filter((t) => consoleKindOf(t, sourceCategory) === kind).length;
  const visibleTriggers = useMemo(
    () =>
      kindFilter === 'all'
        ? triggers
        : triggers.filter((t) => consoleKindOf(t, sourceCategory) === kindFilter),
    [triggers, kindFilter, sourceCategory],
  );

  // ── Deliveries: status filter ─────────────────────────────────────────────────
  const [deliveryStatus, setDeliveryStatus] = useState('all');
  const feed = useTriggerDeliveries({ status: deliveryStatus, window: range });

  // A linked delivery opens once its page has loaded, paging back while it is not found yet.
  const linkedRow =
    linkedDelivery === null ? undefined : feed.deliveries.find((d) => d.id === linkedDelivery);
  // A failed page stops the search, so a failing API is not asked again and again.
  const { loading, loadingMore, hasMore, loadOlder, deliveries, error: pageError } = feed;
  useEffect(() => {
    if (linkedDelivery === null || linkedRow !== undefined || loading || loadingMore) return;
    if (pageError !== null) return;
    if (hasMore && deliveries.length < LINKED_DELIVERY_SEARCH) loadOlder();
  }, [
    linkedDelivery,
    linkedRow,
    loading,
    loadingMore,
    pageError,
    hasMore,
    deliveries.length,
    loadOlder,
  ]);
  const shownDelivery = detailDelivery ?? linkedRow ?? null;

  /** Drops the link from the address once its modal closes, so a reload does not reopen it. */
  const forgetLink = useCallback(() => {
    setLinkedDelivery(null);
    if (window.location.search !== '') window.history.replaceState(null, '', APP_ROUTES.triggers);
  }, []);

  const [fireResult, setFireResult] = useState<TriggerNotice | null>(null);

  // Every mutation this page can perform, shared with the agent page's
  // Triggers tab so the two surfaces drive the same modal identically.
  const { retry: reloadFeed } = feed;
  const refresh = useCallback(() => {
    router.refresh();
    reloadFeed();
  }, [router, reloadFeed]);
  const {
    fireManualTrigger,
    startManualRun,
    rotateSecret,
    toggleTrigger,
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
      if (result.ok || result.createdSource) router.refresh();
      return result;
    },
    [router, ingestHost],
  );

  // Read the open trigger back out of the live list rather than trusting the
  // row captured when the modal opened: saving a setting inside the panel
  // refreshes the page data, and a snapshot would leave the control showing
  // the value it had before the save.
  const modalRow =
    modalTrigger === null ? null : (triggers.find((t) => t.id === modalTrigger.id) ?? modalTrigger);

  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Stack direction="horizontal" justify="between" align="start">
          <Stack gap="tight" maxWidth="prose">
            <Heading variant="section">Triggers</Heading>
            <Text size="supporting" color="muted">
              Everything that starts work on the platform: inbound webhooks, chat commands,
              schedules and manual fires today, with control-plane events and chained runs coming.
              Every delivery is recorded.
            </Text>
          </Stack>
          <SegmentedControl
            aria-label="Period"
            value={range}
            onChange={setRange}
            options={STATS_RANGE_OPTIONS}
          />
        </Stack>
      </Stack>

      {locked && <ManagedInGitCallout subject="Triggers and sources in this tenant" />}
      {error && <Alert variant="error">{error}</Alert>}
      {fireResult && <Alert variant={fireResult.ok ? 'info' : 'error'}>{fireResult.message}</Alert>}

      <TriggerStatsKpis triggers={triggers} stats={stats} window={range} />

      {/* Sources are not a headline surface: a webhook source is created
          inside the New Trigger flow and managed from its trigger's modal,
          so the page stays about what starts work. */}
      <Tabs defaultValue={initialTab}>
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
                  onChange={(v) => setKindFilter(v as ConsoleKind | 'all')}
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
                      count: kindCount('webhook'),
                    },
                    {
                      value: 'chat',
                      label: 'Chat command',
                      icon: <Icon name="send" size="2xs" />,
                      count: kindCount('chat'),
                    },
                    {
                      value: 'scheduled',
                      label: 'Schedule',
                      icon: <Icon name="clock" size="2xs" />,
                      count: kindCount('scheduled'),
                    },
                    {
                      value: 'manual',
                      label: 'Manual',
                      icon: <Icon name="play" size="2xs" />,
                      count: kindCount('manual'),
                    },
                  ]}
                />
              }
              toolbarActions={
                locked ? undefined : (
                  <PermissionHint allowed={canWrite} action="create triggers">
                    <Button size="sm" disabled={!canWrite} onClick={() => setWizardOpen(true)}>
                      <Icon name="plus" size="xs" />
                      New trigger
                    </Button>
                  </PermissionHint>
                )
              }
              resetKey={kindFilter}
              window={statsWindowOf(range)}
              triggers={visibleTriggers}
              sourceName={sourceName}
              sourceCategory={sourceCategory}
              sourceWaiting={sourceWaiting}
              onOpen={setModalTrigger}
              onToggle={(t) => void toggleTrigger(t)}
              canRunAgent={canRunAgent}
              emptyMessage={
                triggers.length === 0
                  ? 'No triggers yet.'
                  : `No ${kindFilter === 'all' ? '' : KIND_FILTER_NOUN[kindFilter] + ' '}triggers.`
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
                value={deliveryStatus}
                onChange={(e) => setDeliveryStatus(e.target.value)}
              >
                <option value="all">All statuses</option>
                {DELIVERY_STATUS_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </NativeSelect>
            }
            resetKey={`${deliveryStatus}/${range}`}
            deliveries={feed.deliveries}
            paging={feed}
            sourceName={sourceName}
            onOpen={setDetailDelivery}
            emptyMessage={
              deliveryStatus === 'all'
                ? `No deliveries${rangeSuffix(range)}. Arrivals and their outcomes appear here once a source receives events, a schedule ticks, or a trigger is fired.`
                : `No ${deliveryStatus} deliveries${rangeSuffix(range)}.`
            }
          />
        </TabsContent>
      </Tabs>

      {/* One delivery's payload and outcome. Opening the trigger swaps the
          panel for the owning trigger's modal. */}
      {shownDelivery !== null && (
        <DeliveryDetailPanel
          delivery={shownDelivery}
          onClose={() => {
            setDetailDelivery(null);
            forgetLink();
          }}
          onRedeliver={redeliver}
          redeliverAllowed={canRunOwner(shownDelivery.triggerId)}
          agents={agents}
          runHref={runHrefFor(triggers.find((t) => t.id === shownDelivery.triggerId))}
          onOpenTrigger={(triggerId) => {
            const owner = triggers.find((t) => t.id === triggerId);
            setDetailDelivery(null);
            forgetLink();
            if (owner !== undefined) setModalTrigger(owner);
          }}
        />
      )}

      {/* Modals */}
      <TriggerDetailModal
        trigger={modalRow}
        canRunAgent={modalRow !== null && canRunTrigger(modalRow)}
        sources={sources}
        ingestHost={ingestHost}
        onClose={() => {
          setModalTrigger(null);
          forgetLink();
        }}
        onToggleEnabled={(t) => {
          setModalTrigger(null);
          void toggleTrigger(t);
        }}
        onDelete={(t) => {
          setModalTrigger(null);
          setDeleteCandidate(t);
        }}
        onEdit={saveTriggerEdit}
        onRedeliver={redeliver}
        onRotateSecret={rotateSecret}
        onFire={startManualRun}
        // Scoped to the trigger on screen: firing one trigger must not make
        // another one's panel claim it is queueing.
        firing={firingId === modalRow?.id}
        agents={agents}
        window={range}
      />

      {/* One modal owns the whole creation flow: kind picker, the kind's own
          steps from the registry, the shared steps, and the success screen. */}
      {wizardOpen && (
        <TriggerWizard
          agents={agents}
          agentsError={agentsError}
          sources={wizardSources}
          ingestHost={ingestHost}
          allowRouting
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
