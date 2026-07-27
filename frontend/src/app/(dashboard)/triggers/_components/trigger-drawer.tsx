/** @tier 3 — triggers-scoped. Everything about one trigger, in one place. */

'use client';

import { useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  FieldRow,
  Grid,
  Icon,
  IconTile,
  SectionLabel,
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
  Toggle,
} from '@orbit/terra';
import { CopyButton, StatusBadge } from '@orbit/components';
import { deliveryStatusTone } from '../_types';
import type { DeliveryRow, SourceRow, TriggerRow } from '../_types';
import { recentFailures } from '../_types/fleet';

interface TriggerDrawerProps {
  trigger: TriggerRow | null;
  sources: SourceRow[];
  deliveries: DeliveryRow[];
  /** Public base URL webhook senders post to. */
  ingestHost: string;
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
  if (iso === null) return 'Never';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

/** Renders a cron expression as something a person can check at a glance. */
function describeCron(expr: string): string {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return 'Custom schedule';
  const [min, hour, dom, mon, dow] = parts;
  if (min === '0' && hour === '*' && dom === '*' && mon === '*' && dow === '*') {
    return 'Every hour, on the hour';
  }
  if (dom === '*' && mon === '*' && dow === '*' && /^\d+$/.test(hour) && /^\d+$/.test(min)) {
    return `Every day at ${hour.padStart(2, '0')}:${min.padStart(2, '0')}`;
  }
  if (min.startsWith('*/')) return `Every ${min.slice(2)} minutes`;
  return 'Custom schedule';
}

/** A small figure in the drawer's stat trio. */
function DrawerStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: 'healthy' | 'error';
}) {
  return (
    <Card inset border="subtle">
      <CardContent>
        <Stack gap="tight">
          <SectionLabel>{label}</SectionLabel>
          <Text
            mono
            weight="heavy"
            color={
              tone === 'error' ? 'status-error' : tone === 'healthy' ? 'status-healthy' : undefined
            }
          >
            {value}
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}

/**
 * TriggerDrawer shows one trigger's configuration, history and settings.
 *
 * Split three ways because those are three different questions: what is this,
 * what has it done, and how should it behave. The Overview leads with the thing
 * an operator most often came for — the endpoint to paste into a provider, the
 * schedule to sanity-check, the form to run.
 */
export function TriggerDrawer({
  trigger,
  sources,
  deliveries,
  ingestHost,
  onClose,
  onToggleEnabled,
  onDelete,
  onFire,
  firing = false,
}: TriggerDrawerProps) {
  const [expanded, setExpanded] = useState<string | null>(null);
  const [secretShown, setSecretShown] = useState(false);

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
  const succeeded = mine.reduce(
    (n, d) => n + d.runs.filter((r) => r.triggerId === trigger.id && r.status === 'dispatched').length,
    0,
  );
  const shown = mine.slice(0, DELIVERY_SAMPLE);
  const endpoint = `https://${ingestHost}/api/v1/webhooks/${trigger.sourceId}`;

  const subtitle =
    trigger.kind === 'webhook'
      ? `Webhook · ${source?.name ?? trigger.sourceId} · ${trigger.eventType || 'any event'}`
      : trigger.kind === 'scheduled'
        ? `Schedule · ${trigger.scheduleConfig?.cronExpression ?? ''}`
        : 'Manual · run on demand';

  return (
    <SidePanel open onClose={onClose} size="lg">
      <SidePanelHeader
        title={trigger.name}
        description={subtitle}
        actions={
          <Toggle
            checked={trigger.enabled}
            onChange={(next) => onToggleEnabled(trigger, next)}
            aria-label={`${trigger.enabled ? 'Disable' : 'Enable'} ${trigger.name}`}
          />
        }
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
                  failures. Fix the cause, then re-enable it above.
                </Alert>
              )}

              {/* Webhook: the address to paste into a provider, and the secret
                  that proves deliveries came from it. */}
              {trigger.kind === 'webhook' && (
                <Stack gap="snug">
                  <SectionLabel>Endpoint</SectionLabel>
                  <FieldRow
                    leading={<Icon name="lock" size="2xs" color="status-healthy" />}
                    actions={
                      <CopyButton value={endpoint} label="Copy" />
                    }
                  >
                    <Text mono size="caption">
                      {endpoint}
                    </Text>
                  </FieldRow>

                  <FieldRow
                    leading={
                      <Text mono size="micro" color="subtle">
                        secret
                      </Text>
                    }
                    actions={
                      <>
                        <Button
                          variant="ghost"
                          size="xs"
                          onClick={() => setSecretShown((v) => !v)}
                          // The secret is write-once at creation and never
                          // returned on reads, so there is nothing to reveal.
                          // Saying so beats a button that appears to do nothing.
                          disabled
                        >
                          {secretShown ? 'Hide' : 'Reveal'}
                        </Button>
                        <Button variant="ghost" size="xs" disabled>
                          Rotate
                        </Button>
                      </>
                    }
                  >
                    <Text mono size="caption" color="muted">
                      shown once at creation
                    </Text>
                  </FieldRow>

                  <Text size="caption" color="subtle">
                    Deliveries are verified with an HMAC signature over the raw body. Unsigned or
                    stale requests are rejected before any run starts, and are counted on the source
                    rather than recorded as deliveries.
                  </Text>
                </Stack>
              )}

              {/* Schedule: the expression, in words, with its timezone. */}
              {trigger.kind === 'scheduled' && trigger.scheduleConfig && (
                <Stack gap="snug">
                  <SectionLabel>Schedule</SectionLabel>
                  <FieldRow leading={<Icon name="clock" size="2xs" color="accent" />}>
                    <Stack gap="none">
                      <Text mono size="supporting" weight="emphasis">
                        {trigger.scheduleConfig.cronExpression}
                      </Text>
                      <Text size="caption" color="muted">
                        {describeCron(trigger.scheduleConfig.cronExpression)} ·{' '}
                        {trigger.scheduleConfig.timezone}
                      </Text>
                    </Stack>
                  </FieldRow>
                  <Grid cols={2} gap="snug">
                    <DrawerStat
                      label="Missed runs"
                      value={
                        trigger.scheduleConfig.catchUp ? 'Run on recovery' : 'Recorded and skipped'
                      }
                    />
                    <DrawerStat label="Timezone" value={trigger.scheduleConfig.timezone} />
                  </Grid>
                </Stack>
              )}

              {/* Manual: the form this trigger publishes, and the way to run it. */}
              {trigger.kind === 'manual' && (
                <Stack gap="snug">
                  <SectionLabel>Run this agent</SectionLabel>
                  <Card inset border="subtle" divide column>
                    {(trigger.manualInputs ?? []).map((f) => (
                      <Stack
                        key={f.name}
                        direction="horizontal"
                        align="center"
                        justify="between"
                        gap="snug"
                        padding="snug"
                      >
                        <Stack gap="none">
                          <Text size="supporting" weight="emphasis">
                            {f.label ?? f.name}
                          </Text>
                          <Text mono size="micro" color="subtle">
                            {f.name}
                            {f.required === true ? ' · required' : ''}
                          </Text>
                        </Stack>
                        <Text size="caption" color="muted">
                          {f.type === 'choice' ? (f.choices ?? []).join(' · ') : f.type}
                        </Text>
                      </Stack>
                    ))}
                    <Stack
                      direction="horizontal"
                      justify="between"
                      align="center"
                      padding="snug"
                    >
                      <Text mono size="micro" color="subtle">
                        POST /api/v1/triggers/{trigger.id}/fire
                      </Text>
                      {onFire !== undefined && (
                        <Button
                          size="sm"
                          disabled={!trigger.enabled || firing}
                          onClick={() => onFire(trigger)}
                        >
                          {firing ? 'Queueing…' : 'Run now'}
                        </Button>
                      )}
                    </Stack>
                  </Card>
                </Stack>
              )}

              {/* Event filters: what this trigger accepts of what arrives. */}
              {trigger.kind === 'webhook' && (
                <Stack gap="snug">
                  <SectionLabel>Event filters</SectionLabel>
                  <Stack direction="horizontal" gap="tight" wrap>
                    <Badge variant="default" mono>
                      {trigger.eventType || 'any event'}
                    </Badge>
                    {Object.entries(trigger.matchCondition).map(([k, vals]) => (
                      <Badge key={k} variant="elevated" mono>
                        {k}={vals.join('|')}
                      </Badge>
                    ))}
                  </Stack>
                </Stack>
              )}

              {/* Target: what gets started, and how many at once. */}
              <Stack gap="snug">
                <SectionLabel>Target</SectionLabel>
                <FieldRow
                  leading={
                    <IconTile tone="accent">
                      <Text mono size="supporting" weight="heavy">
                        {trigger.agentSlug.charAt(0).toUpperCase()}
                      </Text>
                    </IconTile>
                  }
                  actions={
                    <Text size="caption" color="subtle">
                      {trigger.maxParallel} parallel · {trigger.overflow}
                    </Text>
                  }
                >
                  <Stack gap="none">
                    <Text size="supporting" weight="emphasis">
                      {trigger.agentSlug}
                    </Text>
                    <Text mono size="micro" color="subtle">
                      agt/{trigger.agentSlug}
                    </Text>
                  </Stack>
                </FieldRow>
              </Stack>

              <Grid cols={3} gap="snug">
                <DrawerStat label="Deliveries 24h" value={String(mine.length)} />
                <DrawerStat label="Succeeded" value={String(succeeded)} tone="healthy" />
                <DrawerStat
                  label="Failed"
                  value={String(failures)}
                  tone={failures > 0 ? 'error' : undefined}
                />
              </Grid>
            </Stack>
          </TabsContent>

          {/* ── Deliveries: what it has actually done ───────────────────── */}
          <TabsContent value="deliveries">
            <Stack gap="comfortable">
              {/* Honest about sampling: never imply this is everything. */}
              <Stack direction="horizontal" justify="between" align="center">
                <Text size="supporting" color="muted">
                  {mine.length === 0
                    ? 'No deliveries yet'
                    : `Last ${shown.length} of ${mine.length}`}
                </Text>
                {mine.length > 0 && (
                  <Text size="caption" color="subtle">
                    Click a row for the payload
                  </Text>
                )}
              </Stack>
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
                              <Text size="caption" color="muted">
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
                                <Text size="caption" color="muted">
                                  {outcome.error}
                                </Text>
                              )}
                              {outcome?.runId != null && (
                                <Text mono size="caption">
                                  run {outcome.runId}
                                </Text>
                              )}
                              {d.payloadPurged ? (
                                <Text size="caption" color="subtle">
                                  Payload purged past this tenant&apos;s retention window. What
                                  happened is still recorded.
                                </Text>
                              ) : (
                                d.payloadExcerpt != null && (
                                  <Text mono size="caption" color="muted">
                                    {d.payloadExcerpt}
                                  </Text>
                                )
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
              <Text size="caption" color="subtle">
                Changes take effect on the next delivery, not on work already in flight.
              </Text>

              <Grid cols={2} gap="snug">
                <DrawerStat label="Runs at once" value={String(trigger.maxParallel)} />
                <DrawerStat
                  label="At the limit"
                  value={
                    trigger.overflow === 'wait'
                      ? `Wait (${trigger.maxQueueDepth} max)`
                      : trigger.overflow === 'replace'
                        ? 'Replace waiting'
                        : 'Refuse'
                  }
                />
                <DrawerStat label="Start timeout" value={`${trigger.timeoutSeconds}s`} />
                <DrawerStat label="Duplicates" value={trigger.dedupeEnabled ? 'Suppressed' : 'Allowed'} />
                <DrawerStat
                  label="Auto-pause"
                  value={trigger.autopauseEnabled ? 'After 10 failures' : 'Off'}
                />
                <DrawerStat
                  label="Consecutive failures"
                  value={String(trigger.consecutiveFailures)}
                  tone={trigger.consecutiveFailures > 0 ? 'error' : undefined}
                />
              </Grid>

              <Separator />

              <Stack direction="horizontal" gap="snug">
                <Button variant="destructive" onClick={() => onDelete(trigger)}>
                  Delete trigger
                </Button>
              </Stack>
              <Text size="caption" color="subtle">
                Deleting drops anything still waiting, recorded as dropped rather than silently
                discarded. Delivery history is kept either way.
              </Text>
            </Stack>
          </TabsContent>
        </Tabs>
      </SidePanelBody>
    </SidePanel>
  );
}
