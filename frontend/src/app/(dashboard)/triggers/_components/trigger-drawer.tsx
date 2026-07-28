/** @tier 3 — triggers-scoped. Everything about one trigger, in one place. */

'use client';

import { useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  DangerZone,
  FieldRow,
  Grid,
  Icon,
  IconTile,
  SectionLabel,
  SettingRow,
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
  ToggleRow,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { CopyButton, EmptyState } from '@orbit/components';
import type { DeliveryRow, SourceRow, TriggerKind, TriggerRow } from '../_types';
import { recentFailures } from '../_types/fleet';
import { DeliveryList } from './delivery-list';

/** Icon per kind, matching the trigger list so a row and its panel agree. */
const KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'webhook',
  scheduled: 'clock',
  manual: 'play',
};

interface TriggerDrawerProps {
  trigger: TriggerRow | null;
  sources: SourceRow[];
  deliveries: DeliveryRow[];
  /** Public base URL webhook senders post to. */
  ingestHost: string;
  onClose: () => void;
  onToggleEnabled: (t: TriggerRow, enabled: boolean) => void;
  onDelete: (t: TriggerRow) => void;
  /**
   * Persists a safety setting. Absent when the caller cannot write, in which
   * case the toggles render disabled rather than pretending to save.
   */
  onUpdateSafety?: (t: TriggerRow, patch: Partial<TriggerRow>) => void;
  /** Fire a manual trigger now. Absent for kinds that fire on their own. */
  onFire?: (t: TriggerRow) => void;
  firing?: boolean;
}

/** How many deliveries the tab shows before it stops. */
const DELIVERY_SAMPLE = 8;

function formatTime(iso: string | null): string {
  if (iso === null) return 'Never';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  // "28 Jul 2026, 08:30" reads at a glance; seconds are noise at this grain.
  return `${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}, ${d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })}`;
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
  prose = false,
}: {
  label: string;
  value: string;
  tone?: 'healthy' | 'error';
  /**
   * Set for values that are words rather than figures. A phrase set in the
   * numeric style reads as a shout; prose values step down to supporting sans.
   */
  prose?: boolean;
}) {
  return (
    <Card inset border="subtle">
      <CardContent padded compact>
        <Stack gap="tight">
          <SectionLabel>{label}</SectionLabel>
          <Text
            mono={!prose}
            size={prose ? 'supporting' : undefined}
            weight={prose ? 'emphasis' : 'heavy'}
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
  onUpdateSafety,
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
  const succeeded = mine.reduce(
    (n, d) =>
      n + d.runs.filter((r) => r.triggerId === trigger.id && r.status === 'dispatched').length,
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
    <SidePanel open onClose={onClose} size="md">
      <SidePanelHeader
        density="compact"
        leading={
          <IconTile size="lg" tone={trigger.autoPausedAt !== null ? 'accent' : 'muted'}>
            <Icon name={KIND_ICON[trigger.kind]} size="xs" />
          </IconTile>
        }
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

      {/* Tabs own the panel's vertical space so the strip sits flush under the
          header with its own full-bleed border, and only the content scrolls. */}
      <Tabs defaultValue="overview" size="compact" fill>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="deliveries">Deliveries</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <SidePanelBody padding="compact">
          {/* ── Overview: what this is and how it fires ─────────────────── */}
          <TabsContent value="overview">
            <Stack gap="comfortable">
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
                    actions={<CopyButton value={endpoint} label="Copy" />}
                  >
                    <Text mono size="caption" truncate>
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
                        {/* Write-once at creation and never returned on reads,
                            so there is nothing to reveal or rotate yet. */}
                        <Button variant="ghost" size="xs" disabled>
                          Reveal
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
                  <FieldRow
                    leading={<Icon name="clock" size="2xs" color="accent" />}
                    actions={
                      <Text mono size="micro" color="subtle">
                        {trigger.scheduleConfig.timezone}
                      </Text>
                    }
                  >
                    <Stack gap="none">
                      <Text mono size="supporting" weight="emphasis">
                        {trigger.scheduleConfig.cronExpression}
                      </Text>
                      <Text size="caption" color="muted">
                        {describeCron(trigger.scheduleConfig.cronExpression)}
                      </Text>
                    </Stack>
                  </FieldRow>
                  <Grid cols={2} gap="snug">
                    <DrawerStat
                      label="Catch-up"
                      value={
                        trigger.scheduleConfig.catchUp ? 'Run on recovery' : 'Skip missed runs'
                      }
                      prose
                    />
                    <DrawerStat label="Created" value={formatTime(trigger.createdAt)} prose />
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
                        padding="cozy"
                      >
                        <Stack gap="none">
                          <Text size="supporting" weight="emphasis">
                            {f.label ?? f.name}
                          </Text>
                          <Text mono size="micro" color="subtle">
                            {f.name} · {f.type}
                          </Text>
                        </Stack>
                        <Text size="micro" color={f.required === true ? 'accent' : 'subtle'}>
                          {f.required === true ? 'required' : 'optional'}
                        </Text>
                      </Stack>
                    ))}
                    <Stack direction="horizontal" justify="between" align="center" padding="cozy">
                      <Text mono size="micro" color="accent" truncate>
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
                  <Stack direction="horizontal" gap="snug" wrap>
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
                      <Text mono size="caption" weight="heavy">
                        {trigger.agentSlug.charAt(0).toUpperCase()}
                      </Text>
                    </IconTile>
                  }
                  actions={
                    <Text size="micro" color="subtle">
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
                <DrawerStat
                  label="Succeeded"
                  value={String(succeeded)}
                  tone={succeeded > 0 ? 'healthy' : undefined}
                />
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
            <Stack gap="snug">
              {mine.length === 0 ? (
                <EmptyState
                  icon="clock"
                  title="No delivery payloads retained"
                  description="This trigger has not fired within the retention window. Deliveries appear here the moment it does."
                />
              ) : (
                <>
                  {/* Honest about sampling: never imply this is everything. */}
                  <Stack direction="horizontal" justify="between" align="center">
                    <Text size="caption" color="muted">
                      Last {shown.length} of {mine.length}
                    </Text>
                    <Text size="caption" color="subtle">
                      Click a row for the payload
                    </Text>
                  </Stack>
                  <DeliveryList
                    deliveries={shown}
                    triggerId={trigger.id}
                    expandedId={expanded}
                    onToggleExpanded={setExpanded}
                  />
                </>
              )}
            </Stack>
          </TabsContent>

          {/* ── Settings: how it should behave ──────────────────────────── */}
          <TabsContent value="settings">
            <Stack gap="comfortable">
              <Stack gap="snug">
                <SectionLabel>Delivery</SectionLabel>
                <Stack gap="snug">
                  <SettingRow
                    label="Runs at once"
                    description="How many runs this trigger may have in flight together."
                    value={String(trigger.maxParallel)}
                  />
                  <SettingRow
                    label="At the limit"
                    description="What happens to an arrival that cannot start yet."
                    value={
                      trigger.overflow === 'wait'
                        ? `Wait · ${trigger.maxQueueDepth} max`
                        : trigger.overflow === 'replace'
                          ? 'Replace waiting'
                          : 'Refuse'
                    }
                  />
                  <SettingRow
                    label="Start timeout"
                    description="How long a queued delivery may wait before it is dropped."
                    value={`${trigger.timeoutSeconds}s`}
                  />
                  <SettingRow
                    label="Consecutive failures"
                    description="Resets to zero the next time a run starts cleanly."
                    value={String(trigger.consecutiveFailures)}
                  />
                </Stack>
              </Stack>

              <Stack gap="snug">
                <SectionLabel>Safety</SectionLabel>
                <Stack gap="snug">
                  <ToggleRow
                    checked={trigger.dedupeEnabled}
                    disabled={onUpdateSafety === undefined}
                    onChange={(next) => onUpdateSafety?.(trigger, { dedupeEnabled: next })}
                    label="Suppress duplicates"
                    description="A repeat of a delivery already seen is recorded, not run again."
                  />
                  <ToggleRow
                    checked={trigger.autopauseEnabled}
                    disabled={onUpdateSafety === undefined}
                    onChange={(next) => onUpdateSafety?.(trigger, { autopauseEnabled: next })}
                    label="Auto-pause on repeated failure"
                    description="Disables the trigger after ten consecutive failures, so a broken target cannot spend the tenant's budget."
                  />
                </Stack>
              </Stack>

              <DangerZone
                title="Delete trigger"
                description="Stops all future deliveries. Anything still waiting is recorded as dropped, and history is retained."
                action={
                  <Button variant="destructive" size="sm" onClick={() => onDelete(trigger)}>
                    Delete
                  </Button>
                }
              />
            </Stack>
          </TabsContent>
        </SidePanelBody>
      </Tabs>
    </SidePanel>
  );
}
