/** @tier 3 — triggers-scoped. Everything about one trigger, in one place. */

'use client';

import { useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  Code,
  Grid,
  Icon,
  Separator,
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
import { StatusBadge } from '@orbit/components';
import { deliveryStatusTone } from '../_types';
import type { DeliveryRow, SourceRow, TriggerRow } from '../_types';
import { recentFailures, sparkline } from '../_types/fleet';
import { TriggerSparkline } from './trigger-sparkline';

interface TriggerDrawerProps {
  trigger: TriggerRow | null;
  sources: SourceRow[];
  deliveries: DeliveryRow[];
  onClose: () => void;
  onToggleEnabled: (t: TriggerRow, enabled: boolean) => void;
  onDelete: (t: TriggerRow) => void;
  /** Fire a manual trigger now. Absent for kinds that fire on their own. */
  onFire?: (t: TriggerRow) => void;
  firing?: boolean;
}

/** How many deliveries the tab shows before it stops. */
const DELIVERY_SAMPLE = 8;

function formatTime(iso: string | null): string {
  if (!iso) return 'Never';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

/** A labelled value in the overview grid. */
function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack gap="tight">
      <Text size="supporting" color="muted">
        {label}
      </Text>
      {children}
    </Stack>
  );
}

/**
 * TriggerDrawer shows one trigger's configuration, history and settings.
 *
 * Split three ways because those are three different questions: what is this,
 * what has it done, and how should it behave. Mixing them is what made the old
 * single list unusable for debugging.
 */
export function TriggerDrawer({
  trigger,
  sources,
  deliveries,
  onClose,
  onToggleEnabled,
  onDelete,
  onFire,
  firing = false,
}: TriggerDrawerProps) {
  const [expanded, setExpanded] = useState<string | null>(null);

  const mine = useMemo(
    () =>
      trigger === null
        ? []
        : deliveries.filter(
            (d) => d.triggerId === trigger.id || d.runs.some((r) => r.triggerId === trigger.id),
          ),
    [trigger, deliveries],
  );

  if (trigger === null) return null;

  const source = sources.find((s) => s.id === trigger.sourceId);
  const failures = recentFailures(trigger.id, deliveries);
  const shown = mine.slice(0, DELIVERY_SAMPLE);

  return (
    <SidePanel open onClose={onClose} size="lg">
      <SidePanelHeader
        title={trigger.name}
        description={`${trigger.kind} trigger · starts ${trigger.agentSlug}`}
      />
      <SidePanelBody>
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="deliveries">Deliveries</TabsTrigger>
            <TabsTrigger value="settings">Settings</TabsTrigger>
          </TabsList>

          {/* ── Overview: what this is and how it fires ─────────────────── */}
          <TabsContent value="overview">
            <Stack gap="relaxed">
              {trigger.autoPausedAt !== null && (
                <Alert variant="error">
                  The platform disabled this trigger after {trigger.consecutiveFailures} consecutive
                  failures. Fix the cause, then re-enable it in Settings.
                </Alert>
              )}

              <Grid cols={2} gap="comfortable">
                {trigger.kind === 'webhook' && (
                  <>
                    <Detail label="Source">
                      <Text>{source?.name ?? trigger.sourceId}</Text>
                    </Detail>
                    <Detail label="Fires on">
                      <Code>{trigger.eventType || 'any event'}</Code>
                    </Detail>
                  </>
                )}
                {trigger.kind === 'scheduled' && trigger.scheduleConfig && (
                  <>
                    <Detail label="Schedule">
                      <Code>{trigger.scheduleConfig.cronExpression}</Code>
                    </Detail>
                    <Detail label="Timezone">
                      <Text>{trigger.scheduleConfig.timezone}</Text>
                    </Detail>
                    <Detail label="Missed fires">
                      <Text>
                        {trigger.scheduleConfig.catchUp
                          ? 'Run on recovery'
                          : 'Recorded and skipped'}
                      </Text>
                    </Detail>
                  </>
                )}
                {trigger.kind === 'manual' && onFire !== undefined && (
                  <Detail label="Run now">
                    <Stack direction="horizontal">
                      <Button
                        size="sm"
                        disabled={!trigger.enabled || firing}
                        onClick={() => onFire(trigger)}
                      >
                        <Icon name="play" size="xs" />
                        {firing ? 'Queueing…' : 'Run now'}
                      </Button>
                    </Stack>
                  </Detail>
                )}
                {trigger.kind === 'manual' && (
                  <Detail label="API path">
                    <Code>POST /api/v1/triggers/{trigger.id}/fire</Code>
                  </Detail>
                )}

                <Detail label="Target agent">
                  <Text>{trigger.agentSlug}</Text>
                </Detail>
                <Detail label="Last 24 hours">
                  <TriggerSparkline
                    buckets={sparkline(trigger.id, deliveries)}
                    label={trigger.name}
                  />
                </Detail>
              </Grid>

              {trigger.kind === 'manual' && (trigger.manualInputs?.length ?? 0) > 0 && (
                <Stack gap="snug">
                  <Text weight="heavy">Inputs</Text>
                  <Text size="supporting" color="muted">
                    One declaration drives this form and the API, so the two cannot drift.
                  </Text>
                  <Stack gap="tight">
                    {trigger.manualInputs?.map((f) => (
                      <Stack key={f.name} direction="horizontal" gap="snug" align="center">
                        <Code>{f.name}</Code>
                        <Badge variant="outline">{f.type}</Badge>
                        {f.required === true && <Badge variant="outline">required</Badge>}
                        {f.choices !== undefined && (
                          <Text size="supporting" color="muted">
                            {f.choices.join(', ')}
                          </Text>
                        )}
                      </Stack>
                    ))}
                  </Stack>
                </Stack>
              )}

              <Separator />

              <Grid cols={3} gap="comfortable">
                <Detail label="Deliveries (24h)">
                  <Text size="lead">{mine.length}</Text>
                </Detail>
                <Detail label="Failures (24h)">
                  <Text size="lead">{failures}</Text>
                </Detail>
                <Detail label="Last fired">
                  <Text>{formatTime(mine[0]?.receivedAt ?? null)}</Text>
                </Detail>
              </Grid>
            </Stack>
          </TabsContent>

          {/* ── Deliveries: what it has actually done ───────────────────── */}
          <TabsContent value="deliveries">
            <Stack gap="comfortable">
              {/* Honest about sampling: never imply this is everything. */}
              <Text size="supporting" color="muted">
                {mine.length === 0
                  ? 'No deliveries yet'
                  : `Showing ${shown.length} of ${mine.length} deliveries`}
              </Text>
              <Stack gap="tight">
                {shown.map((d) => {
                  const outcome = d.runs.find((r) => r.triggerId === trigger.id);
                  const open = expanded === d.id;
                  return (
                    <Card key={d.id}>
                      <CardContent>
                        <Stack gap="tight">
                          <Stack direction="horizontal" justify="between" align="center">
                            <Stack direction="horizontal" gap="snug" align="center">
                              <StatusBadge
                                status={deliveryStatusTone(outcome?.status ?? d.status)}
                                label={outcome?.status ?? d.status}
                              />
                              <Text size="supporting">{d.eventType || d.kind}</Text>
                            </Stack>
                            <Stack direction="horizontal" gap="snug" align="center">
                              <Text size="supporting" color="muted">
                                {formatTime(d.receivedAt)}
                              </Text>
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                onClick={() => setExpanded(open ? null : d.id)}
                                aria-label={open ? 'Hide detail' : 'Show detail'}
                              >
                                <Icon name={open ? 'chevron-up' : 'chevron-down'} size="2xs" />
                              </Button>
                            </Stack>
                          </Stack>
                          {open && (
                            <Stack gap="tight">
                              {outcome?.error != null && (
                                <Text size="supporting" color="muted">
                                  {outcome.error}
                                </Text>
                              )}
                              {outcome?.runId != null && (
                                <Text size="supporting">
                                  Run <Code>{outcome.runId}</Code>
                                </Text>
                              )}
                              {d.payloadPurged ? (
                                <Text size="supporting" color="muted">
                                  Payload purged past this tenant&apos;s retention window. What
                                  happened is still recorded.
                                </Text>
                              ) : (
                                d.payloadExcerpt != null && <Code>{d.payloadExcerpt}</Code>
                              )}
                            </Stack>
                          )}
                        </Stack>
                      </CardContent>
                    </Card>
                  );
                })}
              </Stack>
            </Stack>
          </TabsContent>

          {/* ── Settings: how it should behave ──────────────────────────── */}
          <TabsContent value="settings">
            <Stack gap="relaxed">
              <Text size="supporting" color="muted">
                Changes take effect on the next delivery, not on work already in flight.
              </Text>

              <Grid cols={2} gap="comfortable">
                <Detail label="Runs at once">
                  <Text>{trigger.maxParallel}</Text>
                </Detail>
                <Detail label="When at the limit">
                  <Text>
                    {trigger.overflow === 'wait'
                      ? `Wait (up to ${trigger.maxQueueDepth} queued)`
                      : trigger.overflow === 'replace'
                        ? 'Replace what is waiting'
                        : 'Refuse immediately'}
                  </Text>
                </Detail>
                <Detail label="Start timeout">
                  <Text>{trigger.timeoutSeconds}s</Text>
                </Detail>
                <Detail label="Duplicate suppression">
                  <Text>{trigger.dedupeEnabled ? 'On' : 'Off'}</Text>
                </Detail>
                <Detail label="Auto-pause">
                  <Text>
                    {trigger.autopauseEnabled
                      ? 'After 10 consecutive failures'
                      : 'Off for this trigger'}
                  </Text>
                </Detail>
                <Detail label="Consecutive failures">
                  <Text>{trigger.consecutiveFailures}</Text>
                </Detail>
              </Grid>

              <Separator />

              <Stack direction="horizontal" gap="snug">
                <Button
                  variant="secondary"
                  onClick={() => onToggleEnabled(trigger, !trigger.enabled)}
                >
                  {trigger.enabled ? 'Disable' : 'Enable'}
                </Button>
                <Button variant="destructive" onClick={() => onDelete(trigger)}>
                  Delete trigger
                </Button>
              </Stack>
              <Text size="supporting" color="muted">
                Disabling drops anything still waiting, recorded as dropped rather than silently
                discarded. Delivery history is kept either way.
              </Text>
            </Stack>
          </TabsContent>
        </Tabs>
      </SidePanelBody>
    </SidePanel>
  );
}
