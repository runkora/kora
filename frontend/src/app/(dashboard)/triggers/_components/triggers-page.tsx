'use client';

/** @tier 3 — triggers-scoped. Platform-wide Triggers control surface:
 * Sources / Triggers / Deliveries. */

import { useCallback, useEffect, useMemo, useState } from 'react';
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
} from '@runkora/hades';
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
import { consoleKindOf, matchesStatusFilter, runTargets } from '../_types';
import { ingestHostFromWindow } from '../_lib/endpoints';
import { DISCORD_PROVIDER } from '../_lib/discord-invite';
import type {
  AgentOption,
  ConsoleKind,
  DeliveryRow,
  SourceCategory,
  SourceConnection,
  SourceRow,
  TriggerRow,
} from '../_types';
import { useHasScope } from '../../_components/permissions-provider';
import { PermissionHint } from '@runkora/components';

interface TriggersPageProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
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

/** The kind in the empty-list message, e.g. "No chat command triggers." */
const KIND_FILTER_NOUN: Record<ConsoleKind, string> = {
  webhook: 'webhook',
  chat: 'chat command',
  scheduled: 'scheduled',
  manual: 'manual',
};

/** How often a page with a connecting source asks for fresh data. */
const CONNECTING_REFRESH_MS = 10_000;

export function TriggersPage({
  sources,
  triggers,
  deliveries,
  agents,
  agentsError = null,
  error,
}: TriggersPageProps) {
  const router = useRouter();
  const canWrite = useHasScope('triggers:write');
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
    // A Discord source shows its connection state instead, and none while disabled.
    const waiting = new Set(
      sources
        .filter((s) => !s.signingSecretSet && s.provider !== DISCORD_PROVIDER)
        .map((s) => s.id),
    );
    return (id: string) => waiting.has(id);
  }, [sources]);

  const sourceConnection = useMemo(() => {
    const map = new Map<string, SourceConnection>();
    for (const s of sources) if (s.connection !== null) map.set(s.id, s.connection);
    return (id: string) => map.get(id);
  }, [sources]);

  const wizardSources = useMemo(
    () =>
      sources.map((s) => ({
        id: s.id,
        name: s.name,
        provider: s.provider,
        category: s.category,
        signingSecretSet: s.signingSecretSet,
      })),
    [sources],
  );

  // ── Modal + dialog state ─────────────────────────────────────────────────────
  const [wizardOpen, setWizardOpen] = useState(false);
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  // The platform Deliveries tab is not scoped to one trigger, so a row opens
  // its own panel rather than the owning trigger's drawer: the question being
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

  const visibleDeliveries = useMemo(
    () =>
      deliveryStatus === 'all'
        ? deliveries
        : deliveries.filter((d) => matchesStatusFilter(d, new Set([deliveryStatus]))),
    [deliveries, deliveryStatus],
  );

  const [fireResult, setFireResult] = useState<TriggerNotice | null>(null);

  // Every mutation this page can perform, shared with the agent page's
  // Triggers tab so the two surfaces drive the same drawer identically.
  const refresh = useCallback(() => router.refresh(), [router]);

  // Refresh while a source is connecting: the backend records the session result.
  const anyConnecting = sources.some((s) => s.connection?.state === 'connecting');
  useEffect(() => {
    if (!anyConnecting) return;
    const timer = setInterval(() => router.refresh(), CONNECTING_REFRESH_MS);
    return () => clearInterval(timer);
  }, [anyConnecting, router]);
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
      if (result.ok || result.createdSource) router.refresh();
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
              Everything that starts work on the platform: inbound webhooks, chat commands,
              schedules and manual fires today, with control-plane events and chained runs coming.
              Every delivery is recorded.
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
                <PermissionHint allowed={canWrite} action="create triggers">
                  <Button size="sm" disabled={!canWrite} onClick={() => setWizardOpen(true)}>
                    <Icon name="plus" size="xs" />
                    New trigger
                  </Button>
                </PermissionHint>
              }
              resetKey={kindFilter}
              triggers={visibleTriggers}
              deliveries={deliveries}
              sourceName={sourceName}
              sourceCategory={sourceCategory}
              sourceWaiting={sourceWaiting}
              sourceConnection={sourceConnection}
              onOpen={setDrawerTrigger}
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
            resetKey={deliveryStatus}
            deliveries={visibleDeliveries}
            sourceName={sourceName}
            onOpen={setDetailDelivery}
            emptyMessage={
              deliveries.length === 0
                ? 'No deliveries yet. Arrivals and their outcomes appear here once a source receives events, a schedule ticks, or a trigger is fired.'
                : `No ${deliveryStatus === 'all' ? '' : deliveryStatus + ' '}deliveries.`
            }
          />
        </TabsContent>
      </Tabs>

      {/* One delivery's payload and outcome. Opening the trigger swaps the
          panel for the owning trigger's drawer. */}
      {detailDelivery !== null && (
        <DeliveryDetailPanel
          delivery={detailDelivery}
          onClose={() => setDetailDelivery(null)}
          onRedeliver={redeliver}
          redeliverAllowed={canRunOwner(detailDelivery.triggerId)}
          agents={agents}
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
        canRunAgent={drawerRow !== null && canRunTrigger(drawerRow)}
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
        agents={agents}
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
