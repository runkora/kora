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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import type { DataTableColumn, TableColumnFilterOption } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import {
  createSource,
  createTrigger,
  deleteTrigger,
  fireTrigger,
  updateSource,
  updateTrigger,
} from '../_services/triggers-api';
import { TriggerWizard } from './trigger-wizard';
import type { TriggerCreateResult, TriggerDraft } from './trigger-wizard';
import { FleetKpis } from './fleet-kpis';
import { TriggerDrawer } from './trigger-drawer';
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
  // Webhooks are ingested on the API ingress (api.<host>), not the console's
  // own host: the orbit-api VirtualService routes /api/v1/webhooks/ to
  // orbit-triggers, while the console host redirects to login.
  const ingestHost =
    typeof window === 'undefined' ? INGEST_HOST_FALLBACK : `api.${window.location.host}`;

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

  /**
   * Executes the wizard's draft: the source first when one was asked for, then
   * the trigger. The secret only exists when a source was just created, and it
   * is passed straight through to the done screen; nothing stores it.
   */
  const createFromWizard = useCallback(
    async (draft: TriggerDraft): Promise<TriggerCreateResult> => {
      let sourceId: string | undefined;
      let secret: string | undefined;

      if (draft.kind === 'webhook') {
        if (draft.source === undefined) return { ok: false, error: 'Choose a source.' };
        if ('existingId' in draft.source) {
          sourceId = draft.source.existingId;
        } else {
          const made = await createSource({
            name: draft.source.newName,
            provider: draft.source.provider,
          });
          if (made.error !== null || made.source === null) {
            return { ok: false, error: made.error ?? 'Failed to create the source.' };
          }
          sourceId = made.source.id;
          secret = made.source.secret;
        }
      }

      const made = await createTrigger({
        kind: draft.kind,
        name: draft.name,
        agent_type_id: draft.agentTypeId,
        prompt_template: draft.promptTemplate,
        ...(sourceId !== undefined && { source_id: sourceId }),
        ...(draft.kind === 'webhook' && { event_type: draft.eventType ?? '' }),
        ...(draft.matchCondition !== undefined && { match_condition: draft.matchCondition }),
        ...(draft.schedule !== undefined && {
          schedule_config: {
            cron_expression: draft.schedule.cronExpression,
            timezone: draft.schedule.timezone,
            catch_up: draft.schedule.catchUp,
          },
        }),
        ...(draft.manualInputs !== undefined && {
          manual_config: {
            inputs: draft.manualInputs.map((f) => ({
              name: f.name,
              label: f.label,
              type: f.type,
              required: f.required,
              ...(f.choices !== undefined && { choices: f.choices }),
            })),
          },
        }),
        max_parallel: draft.maxParallel,
        overflow: draft.overflow,
        max_queue_depth: draft.maxQueueDepth,
        timeout_seconds: draft.timeoutSeconds,
        dedupe_enabled: draft.dedupeEnabled,
        autopause_enabled: draft.autopauseEnabled,
      });
      if (made.error !== null || made.trigger === null) {
        // A just-created source outlives the failure on purpose: it is real,
        // and the retry can pick it from the existing-sources list.
        return { ok: false, error: made.error ?? 'Failed to create the trigger.' };
      }

      router.refresh();
      return {
        ok: true,
        triggerId: made.trigger.id,
        ...(sourceId !== undefined && {
          endpoint: `https://${ingestHost}/api/v1/webhooks/${sourceId}`,
        }),
        ...(secret !== undefined && { secret }),
      };
    },
    [router, ingestHost],
  );

  /** Rotates a webhook trigger's source secret; the drawer shows it once. */
  const rotateSecret = useCallback(async (row: TriggerRow): Promise<string | null> => {
    const result = await updateSource(row.sourceId, { rotate_secret: true });
    return result.source?.secret ?? null;
  }, []);

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
        ...(patch.maxParallel !== undefined && { max_parallel: patch.maxParallel }),
        ...(patch.overflow !== undefined && { overflow: patch.overflow }),
        ...(patch.maxQueueDepth !== undefined && { max_queue_depth: patch.maxQueueDepth }),
        ...(patch.timeoutSeconds !== undefined && { timeout_seconds: patch.timeoutSeconds }),
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
        onRotateSecret={rotateSecret}
        onFire={(t) => void fireManualTrigger(t)}
        firing={firingId !== null}
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
