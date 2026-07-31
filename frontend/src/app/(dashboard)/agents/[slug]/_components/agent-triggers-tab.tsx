'use client';

/** @tier 3 — agents/[slug]-scoped. The Triggers page's own list, deliveries and
 * wizard, scoped to THIS agent. This file owns the scoping and nothing else:
 * every row, cell, tone and API call is the Triggers feature's, imported rather
 * than reimplemented, so the two surfaces cannot drift. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Heading, Icon, Stack, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { TriggerWizard } from '../../../triggers/_components/trigger-wizard';
import type {
  TriggerCreateResult,
  TriggerDraft,
} from '../../../triggers/_components/trigger-wizard';
import { TriggerList } from '../../../triggers/_components/trigger-list';
import { DeliveryGrid } from '../../../triggers/_components/delivery-grid';
import { DeliveryDetailPanel } from '../../../triggers/_components/delivery-detail-panel';
import { createFromWizardDraft } from '../../../triggers/_services/create-from-wizard';
import {
  listDeliveries,
  listSources,
  listTriggers,
  redeliverDelivery,
  updateTrigger,
} from '../../../triggers/_services/triggers-api';
import {
  toDeliveryRow,
  toSourceRow,
  toTriggerRow,
  toTriggerUpdateRequest,
} from '../../../triggers/_types';
import type { DeliveryRow, SourceRow, TriggerRow } from '../../../triggers/_types';
import { ingestHostFromWindow } from '../../../triggers/_lib/endpoints';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';

export function AgentTriggersTab() {
  const { agent } = useAgentDetailContext();
  const router = useRouter();
  const ingestHost = ingestHostFromWindow();

  const [sources, setSources] = useState<SourceRow[]>([]);
  const [triggers, setTriggers] = useState<TriggerRow[]>([]);
  const [deliveries, setDeliveries] = useState<DeliveryRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [wizardOpen, setWizardOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [detailDelivery, setDetailDelivery] = useState<DeliveryRow | null>(null);

  const agentId = agent?.id ?? '';

  // Loading starts true and is only ever lowered: a reload after an action is
  // a silent refresh of lists already on screen, not a page-level spinner.
  const load = useCallback(() => {
    if (!agentId) return;
    void Promise.all([listSources(), listTriggers(agentId), listDeliveries()]).then(
      ([src, trg, del]) => {
        setSources(src.sources.map(toSourceRow));
        setTriggers(trg.triggers.map(toTriggerRow));
        setDeliveries(del.deliveries.map(toDeliveryRow));
        // The triggers list is what this tab is for; the other two only
        // decorate it, so their failures do not block the page.
        setLoadError(trg.error);
        setLoading(false);
      },
    );
  }, [agentId]);

  useEffect(() => {
    load();
  }, [load]);

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

  const toggleTrigger = useCallback(
    async (t: TriggerRow) => {
      setActionError(null);
      const result = await updateTrigger(t.id, toTriggerUpdateRequest({ enabled: !t.enabled }));
      if (result.error) {
        setActionError(`${t.name}: ${result.error}`);
        return;
      }
      load();
    },
    [load],
  );

  /**
   * Creates from the wizard's draft, then reloads this page's lists.
   *
   * The orchestration is the Triggers page's, shared rather than reimplemented:
   * a trigger created here is created exactly as one created there.
   */
  const createTriggerForAgent = useCallback(
    async (draft: TriggerDraft): Promise<TriggerCreateResult> => {
      const result = await createFromWizardDraft(draft, { ingestHost });
      if (result.ok) load();
      return result;
    },
    [ingestHost, load],
  );

  const redeliver = useCallback(
    async (row: DeliveryRow): Promise<{ deliveryId: string | null; error: string | null }> => {
      const result = await redeliverDelivery(row.id);
      if (result.error === null) load();
      return result;
    },
    [load],
  );

  if (!agent) return null;

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
        <Button size="sm" onClick={() => setWizardOpen(true)}>
          <Icon name="plus" size="xs" />
          New trigger
        </Button>
      </Stack>

      {loadError && <Alert variant="error">{loadError}</Alert>}
      {actionError && <Alert variant="error">{actionError}</Alert>}

      {!loading && triggers.length === 0 ? (
        <EmptyState
          icon="zap"
          title="No triggers for this agent"
          description="Create a trigger to run this agent automatically from a webhook event, a schedule, or a manual fire."
          action={
            <Button size="sm" onClick={() => setWizardOpen(true)}>
              <Icon name="plus" size="xs" />
              New trigger
            </Button>
          }
        />
      ) : (
        triggers.length > 0 && (
          <TriggerList
            triggers={triggers}
            deliveries={agentDeliveries}
            sourceName={sourceName}
            // Configuring a trigger lives in the Triggers page drawer, which
            // owns every field. This page creates and lists; opening a row
            // goes to the editor rather than forking a second one here.
            onOpen={() => router.push(APP_ROUTES.triggers)}
            onToggle={(t) => void toggleTrigger(t)}
            emptyMessage="No triggers for this agent."
          />
        )
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

      {detailDelivery !== null && (
        <DeliveryDetailPanel
          delivery={detailDelivery}
          onClose={() => setDetailDelivery(null)}
          onRedeliver={redeliver}
        />
      )}

      {wizardOpen && (
        <TriggerWizard
          agents={[
            { id: agent.id, slug: agent.slug, name: agent.name, identityMode: agent.identityMode },
          ]}
          sources={sources.map((src) => ({ id: src.id, name: src.name, provider: src.provider }))}
          ingestHost={ingestHost}
          onCreate={createTriggerForAgent}
          onClose={() => {
            setWizardOpen(false);
            load();
          }}
        />
      )}
    </Stack>
  );
}
