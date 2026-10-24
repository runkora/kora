'use client';

/** @tier 3 — agents/[slug]-scoped. The Triggers page's own list, drawer,
 * deliveries and wizard, scoped to THIS agent. This file owns the scoping and
 * nothing else: every row, cell, tone, action and API call is the Triggers
 * feature's, imported rather than reimplemented, so the two surfaces cannot
 * drift. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  CardContent,
  Heading,
  Icon,
  Skeleton,
  Stack,
  Text,
  ConfirmDialog,
  EmptyState,
} from '@runkora/hades';
import { TriggerWizard } from '../../../triggers/_components/trigger-wizard';
import type {
  TriggerCreateResult,
  TriggerDraft,
} from '../../../triggers/_components/trigger-wizard';
import { TriggerList } from '../../../triggers/_components/trigger-list';
import { TriggerDrawer } from '../../../triggers/_components/trigger-drawer';
import { RunTriggerModal } from '../../../triggers/_components/run-trigger-modal';
import { DeliveryGrid } from '../../../triggers/_components/delivery-grid';
import { DeliveryDetailPanel } from '../../../triggers/_components/delivery-detail-panel';
import type { RoutingAgentName } from '../../../triggers/_components/delivery-routing';
import { useTriggerActions } from '../../../triggers/_components/use-trigger-actions';
import type { TriggerNotice } from '../../../triggers/_components/use-trigger-actions';
import { createFromWizardDraft } from '../../../triggers/_services/create-from-wizard';
import {
  listDeliveries,
  listSources,
  listTriggers,
} from '../../../triggers/_services/triggers-api';
import { runTargets, toDeliveryRow, toSourceRow, toTriggerRow } from '../../../triggers/_types';
import type { DeliveryRow, SourceRow, TriggerRow } from '../../../triggers/_types';
import { ingestHostFromWindow } from '../../../triggers/_lib/endpoints';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { useHasScope, useTenantLocked } from '../../../_components/permissions-provider';
import { ManagedInGitCallout, PermissionHint } from '@runkora/components';
import { fetchAgentTypes } from '../../_services/agents-api';
import type { AgentTypeRow } from '../../_types';

export function AgentTriggersTab() {
  const { agent } = useAgentDetailContext();
  const ingestHost = ingestHostFromWindow();

  const [sources, setSources] = useState<SourceRow[]>([]);
  const [triggers, setTriggers] = useState<TriggerRow[]>([]);
  const [deliveries, setDeliveries] = useState<DeliveryRow[]>([]);
  // Every tenant agent, so a routed trigger's other candidates show by name.
  const [tenantAgents, setTenantAgents] = useState<AgentTypeRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [wizardOpen, setWizardOpen] = useState(false);
  const canWrite = useHasScope('triggers:write');
  const locked = useTenantLocked();
  const [notice, setNotice] = useState<TriggerNotice | null>(null);
  const [drawerTrigger, setDrawerTrigger] = useState<TriggerRow | null>(null);
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);

  const agentId = agent?.id ?? '';

  // Loading starts true and is only ever lowered: a reload after an action is
  // a silent refresh of lists already on screen, not a page-level spinner.
  const load = useCallback(() => {
    if (!agentId) return;
    void Promise.all([
      listSources(),
      listTriggers(agentId),
      listDeliveries(),
      fetchAgentTypes(),
    ]).then(([src, trg, del, types]) => {
      setSources(src.sources.map(toSourceRow));
      setTriggers(trg.triggers.map(toTriggerRow));
      setDeliveries(del.deliveries.map(toDeliveryRow));
      setTenantAgents(types.data);
      // The triggers list is what this tab is for; the others only
      // decorate it, so their failures do not block the page.
      setLoadError(trg.error);
      setLoading(false);
    });
  }, [agentId]);

  useEffect(() => {
    load();
  }, [load]);

  // The same actions the platform Triggers page performs, refreshing this
  // page's lists and raising notices on its own Alert.
  const {
    startManualRun,
    rotateSecret,
    toggleTrigger,
    updateSafety,
    saveTriggerEdit,
    redeliver,
    firingId,
    runFormTrigger,
    setRunFormTrigger,
    fireManualTrigger,
    deleteCandidate,
    setDeleteCandidate,
    deleting,
    deleteError,
    confirmDelete,
    closeDeleteDialog,
  } = useTriggerActions({ refresh: load, notify: setNotice });

  // The delivery log is platform-wide; this page shows the arrivals that
  // reached THIS agent's triggers, whether as the matched trigger or as one
  // row of a fan-out.
  const triggerIds = useMemo(() => new Set(triggers.map((t) => t.id)), [triggers]);
  const agentDeliveries = useMemo(
    () =>
      deliveries.filter(
        (d) =>
          (d.triggerId !== null && triggerIds.has(d.triggerId)) ||
          d.runs.some((r) => triggerIds.has(r.triggerId)),
      ),
    [deliveries, triggerIds],
  );

  const sourceName = useMemo(() => {
    const map = new Map(sources.map((s) => [s.id, s.name]));
    return (id: string) => map.get(id) ?? id;
  }, [sources]);

  const sourceCategory = useMemo(() => {
    const map = new Map(sources.map((s) => [s.id, s.category]));
    return (id: string) => map.get(id);
  }, [sources]);

  const sourceWaiting = useMemo(() => {
    const waiting = new Set(sources.filter((s) => !s.signingSecretSet).map((s) => s.id));
    return (id: string) => waiting.has(id);
  }, [sources]);

  /**
   * Creates from the wizard's draft, then reloads this page's lists.
   *
   * The orchestration is the Triggers page's, shared rather than reimplemented:
   * a trigger created here is created exactly as one created there.
   */
  const createTriggerForAgent = useCallback(
    async (draft: TriggerDraft): Promise<TriggerCreateResult> => {
      const result = await createFromWizardDraft(draft, { ingestHost });
      if (result.ok || result.createdSource) load();
      return result;
    },
    [ingestHost, load],
  );

  // Read the open trigger back out of the live list rather than trusting the
  // row captured when the drawer opened: saving a setting inside the panel
  // reloads the lists, and a snapshot would show the value it had before.
  const drawerRow =
    drawerTrigger === null
      ? null
      : (triggers.find((t) => t.id === drawerTrigger.id) ?? drawerTrigger);

  if (!agent) return null;

  // This agent still names itself when the tenant list failed to load.
  const agentNames: RoutingAgentName[] = tenantAgents.length > 0 ? tenantAgents : [agent];
  // A routed trigger needs run authority on every candidate, not only this agent.
  const canRunAgent = (id: string) =>
    id === agent.id ? agent.canRun : tenantAgents.some((a) => a.id === id && a.canRun);
  const canRunTrigger = (t: TriggerRow | null | undefined) =>
    t != null && runTargets(t).every(canRunAgent);

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

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Heading variant="section">Triggers</Heading>
          <Text size="supporting" color="muted">
            Everything that starts this agent automatically: webhooks, schedules and manual fires.
            Managed by platform admins.
          </Text>
        </Stack>
        {!locked && (
          <PermissionHint allowed={canWrite} action="create triggers">
            <Button size="sm" disabled={!canWrite} onClick={() => setWizardOpen(true)}>
              <Icon name="plus" size="xs" />
              New trigger
            </Button>
          </PermissionHint>
        )}
      </Stack>

      {locked && <ManagedInGitCallout subject="Triggers in this tenant" />}
      {loadError && <Alert variant="error">{loadError}</Alert>}
      {notice && <Alert variant={notice.ok ? 'info' : 'error'}>{notice.message}</Alert>}

      {loading ? (
        <TriggerListSkeleton />
      ) : loadError ? null : triggers.length === 0 ? (
        <EmptyState
          icon="zap"
          title="No triggers for this agent"
          description="Create a trigger to run this agent automatically from a webhook event, a schedule, or a manual fire."
          action={
            locked ? undefined : (
              <PermissionHint allowed={canWrite} action="create triggers">
                <Button size="sm" disabled={!canWrite} onClick={() => setWizardOpen(true)}>
                  <Icon name="plus" size="xs" />
                  New trigger
                </Button>
              </PermissionHint>
            )
          }
        />
      ) : (
        <TriggerList
          triggers={triggers}
          deliveries={agentDeliveries}
          sourceName={sourceName}
          sourceCategory={sourceCategory}
          sourceWaiting={sourceWaiting}
          onOpen={setDrawerTrigger}
          onToggle={(t) => void toggleTrigger(t)}
          canRunAgent={canRunAgent}
          emptyMessage="No triggers for this agent."
        />
      )}

      {agentDeliveries.length > 0 && (
        <Stack gap="snug">
          <Heading variant="section">Recent deliveries</Heading>
          <DeliveryGrid
            deliveries={agentDeliveries}
            sourceName={sourceName}
            onOpen={setDetailDelivery}
            emptyMessage="No deliveries for this agent yet."
          />
        </Stack>
      )}

      {/* The Triggers page's own drawer, so configuring a trigger here is the
          same surface, byte for byte, as configuring it there. */}
      <TriggerDrawer
        trigger={drawerRow}
        canRunAgent={canRunTrigger(drawerRow)}
        sources={sources}
        deliveries={agentDeliveries}
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
        agents={agentNames}
      />

      {detailDelivery !== null && (
        <DeliveryDetailPanel
          delivery={detailDelivery}
          onClose={() => setDetailDelivery(null)}
          onRedeliver={redeliver}
          redeliverAllowed={canRunTrigger(triggers.find((t) => t.id === detailDelivery.triggerId))}
          agents={agentNames}
          onOpenTrigger={(triggerId) => {
            const owner = triggers.find((t) => t.id === triggerId);
            setDetailDelivery(null);
            if (owner !== undefined) setDrawerTrigger(owner);
          }}
        />
      )}

      {wizardOpen && (
        <TriggerWizard
          agents={[
            {
              id: agent.id,
              slug: agent.slug,
              name: agent.name,
              identityMode: agent.identityMode,
              canRun: agent.canRun,
            },
          ]}
          sources={sources.map((src) => ({
            id: src.id,
            name: src.name,
            provider: src.provider,
            category: src.category,
          }))}
          ingestHost={ingestHost}
          onCreate={createTriggerForAgent}
          onClose={() => {
            setWizardOpen(false);
            load();
          }}
        />
      )}

      {runFormTrigger !== null && (
        <RunTriggerModal
          trigger={runFormTrigger}
          onClose={() => setRunFormTrigger(null)}
          onFire={(variables) => fireManualTrigger(runFormTrigger, variables)}
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
        onClose={closeDeleteDialog}
      />
    </Stack>
  );
}

/**
 * Stands in for the trigger list during the first load only, so the tab never
 * sits blank under its header. Later reloads keep the lists on screen.
 */
function TriggerListSkeleton() {
  return (
    <Card column aria-busy="true">
      <CardContent padded>
        <Stack gap="snug" role="status" aria-label="Loading triggers">
          {Array.from({ length: 3 }).map((_, i) => (
            <Stack key={i} direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="line" width="quarter" size="sm" />
              <Skeleton shape="line" width="half" size="sm" />
              <Skeleton shape="line" width="quarter" size="sm" />
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
