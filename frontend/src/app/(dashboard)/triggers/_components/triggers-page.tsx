'use client';

/** @tier 3 — triggers-scoped. Platform-wide Triggers control surface:
 * Sources / Triggers / Deliveries. */

import { useCallback, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  Input,
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
} from '@orbit/terra';
import { ConfirmDialog } from '@orbit/components';
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
import { deliveryTriggerLabel, matchesStatusFilter } from '../_types';
import { ingestHostFromWindow } from '../_lib/endpoints';
import type { AgentOption, DeliveryRow, SourceRow, TriggerKind, TriggerRow } from '../_types';

interface TriggersPageProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
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
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  // The platform Deliveries tab is not scoped to one trigger, so a row opens
  // its own panel rather than the owning trigger's drawer: the question being
  // asked is "what did this delivery do", not "how is that trigger set up".
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);
  const [kindFilter, setKindFilter] = useState<TriggerKind | 'all'>('all');
  const [query, setQuery] = useState('');

  // The grid renders the toolbar but owns none of it, so the page holds both the
  // kind filter and the search and hands down the rows that survived them.
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

  // ── Deliveries: status filter and search ─────────────────────────────────────
  const [deliveryStatus, setDeliveryStatus] = useState('all');
  const [deliveryQuery, setDeliveryQuery] = useState('');

  const visibleDeliveries = useMemo(() => {
    const q = deliveryQuery.trim().toLowerCase();
    const selected = new Set([deliveryStatus]);
    return deliveries.filter((d) => {
      if (deliveryStatus !== 'all' && !matchesStatusFilter(d, selected)) return false;
      if (q === '') return true;
      // Searched over what the row actually shows, plus the id, so a delivery
      // quoted in a log line can be found by pasting it in.
      return [d.eventType, d.status, deliveryTriggerLabel(d), sourceName(d.sourceId), d.id].some(
        (field) => field.toLowerCase().includes(q),
      );
    });
  }, [deliveries, deliveryStatus, deliveryQuery, sourceName]);

  const deliveryFilterRow = (
    <Stack direction="horizontal" align="center" justify="between" gap="cozy" grow>
      <Stack maxWidth="form-field">
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
      </Stack>
      <Stack grow maxWidth="search-field">
        <Input
          value={deliveryQuery}
          onChange={(e) => setDeliveryQuery(e.target.value)}
          placeholder="Search deliveries…"
          aria-label="Search deliveries"
        />
      </Stack>
    </Stack>
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
          <DeliveryGrid
            toolbar={deliveryFilterRow}
            resetKey={`${deliveryStatus}|${deliveryQuery.trim()}`}
            deliveries={visibleDeliveries}
            sourceName={sourceName}
            onOpen={setDetailDelivery}
            emptyMessage={
              deliveries.length === 0
                ? 'No deliveries yet. Arrivals and their outcomes appear here once a source receives events, a schedule ticks, or a trigger is fired.'
                : deliveryQuery.trim() !== ''
                  ? 'No deliveries match this search.'
                  : `No ${deliveryStatus} deliveries.`
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
