/** @tier 3 — triggers-scoped. Everything about one trigger, in one place. */

'use client';

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  CardContent,
  DangerZone,
  FieldRow,
  Grid,
  Icon,
  IconTile,
  Input,
  NativeSelect,
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
  TemplateField,
  Text,
  Toggle,
  ToggleRow,
  SegmentedControl,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { CopyButton, EmptyState } from '@orbit/components';
import type { DeliveryRow, SourceRow, TriggerKind, TriggerRow } from '../_types';
import { recentFailures } from '../_types/fleet';
import { DeliveryList } from './delivery-list';
import {
  catalogPatterns,
  scheduledCatalog,
  toTemplateCatalog,
  webhookCatalog,
} from './trigger-wizard/field-catalog';
import {
  describeMatchCondition,
  matchConditionFrom,
  splitFilterValues,
} from './trigger-wizard/wizard-state';
import { resolveBrowserZone, timezoneOptions } from './trigger-wizard/cron';
import {
  eventOptionsFor,
  filtersFromMatchCondition,
  isValidCron,
  manualInputsCatalog,
  sameFilters,
} from './trigger-edit';
import type { EditFilterRow } from './trigger-edit';
import { useEditableSection } from './use-editable-section';

/** Icon per kind, matching the trigger list so a row and its panel agree. */
const KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'webhook',
  scheduled: 'clock',
  manual: 'play',
};

/**
 * A correction to a stored trigger. Every field is optional: a section saves
 * only what it owns, so renaming a trigger cannot disturb its filters.
 */
export interface TriggerEditPatch {
  name?: string;
  promptTemplate?: string;
  /** Empty string means any event type. */
  eventType?: string;
  matchCondition?: Record<string, string[]>;
  scheduleConfig?: { cronExpression: string; timezone: string; catchUp: boolean };
}

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
   * Persists a settings change (safety flags and admission tuning). Absent
   * when the caller cannot write, in which case the controls render disabled
   * rather than pretending to save.
   */
  onUpdateSafety?: (t: TriggerRow, patch: Partial<TriggerRow>) => void;
  /**
   * Persists a correction to the trigger's configuration, resolving with the
   * error string or null. Absent when the caller cannot write, in which case
   * the edit affordances are not offered at all: a Save that cannot save is
   * worse than a read-only panel.
   */
  onEdit?: (t: TriggerRow, patch: TriggerEditPatch) => Promise<string | null>;
  /**
   * Replays a delivery, resolving with the new delivery's id or an error.
   * Absent for read-only callers, where no Redeliver is offered.
   */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
  /**
   * Rotates the webhook source's signing secret and resolves with the new one,
   * which is shown once here and never again. Absent for read-only callers.
   */
  onRotateSecret?: (t: TriggerRow) => Promise<string | null>;
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

/**
 * A compact − n + control for a bounded numeric setting, saving on each step.
 * Matches the wizard's stepper so the two surfaces feel like one product.
 */
function StepperControl({
  value,
  min,
  max,
  step = 1,
  suffix = '',
  label,
  disabled,
  onChange,
}: {
  value: number;
  min: number;
  max: number;
  step?: number;
  suffix?: string;
  label: string;
  disabled: boolean;
  onChange: (next: number) => void;
}) {
  return (
    <Stack direction="horizontal" align="center" gap="tight">
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={disabled || value <= min}
        aria-label={`Decrease ${label}`}
        onClick={() => onChange(Math.max(min, value - step))}
      >
        <Icon name="minus" size="2xs" />
      </Button>
      <Text as="span" mono size="supporting" weight="heavy">
        {value}
        {suffix}
      </Text>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={disabled || value >= max}
        aria-label={`Increase ${label}`}
        onClick={() => onChange(Math.min(max, value + step))}
      >
        <Icon name="plus" size="2xs" />
      </Button>
    </Stack>
  );
}

/**
 * The save affordance a dirty section grows.
 *
 * Nothing is shown until something changes, so a panel being read stays a panel
 * being read. A blocker replaces the button rather than disabling it, because a
 * dead button does not say what is wrong.
 */
function EditActions({
  dirty,
  pending,
  error,
  blocker,
  saveLabel,
  onSave,
  onDiscard,
}: {
  dirty: boolean;
  pending: boolean;
  error: string | null;
  /** Why this cannot be saved yet, if anything. */
  blocker?: string;
  saveLabel: string;
  onSave: () => void;
  onDiscard: () => void;
}) {
  if (!dirty) return null;
  return (
    <Stack gap="snug">
      {error !== null && <Alert variant="error">{error}</Alert>}
      <Stack direction="horizontal" align="center" justify="end" gap="snug">
        {blocker !== undefined && (
          <Text size="caption" color="status-error">
            {blocker}
          </Text>
        )}
        <Button variant="ghost" size="xs" disabled={pending} onClick={onDiscard}>
          Discard
        </Button>
        <Button size="xs" disabled={pending || blocker !== undefined} onClick={onSave}>
          {pending ? 'Saving…' : saveLabel}
        </Button>
      </Stack>
    </Stack>
  );
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
  onEdit,
  onRedeliver,
  onRotateSecret,
  onFire,
  firing = false,
}: TriggerDrawerProps) {
  const [expanded, setExpanded] = useState<string | null>(null);
  // Rotation is destructive (the provider's configured secret stops working the
  // moment it lands), so the button asks once before doing it.
  const [rotateArmed, setRotateArmed] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [rotatedSecret, setRotatedSecret] = useState<string | null>(null);

  const mine = useMemo(
    () =>
      trigger === null
        ? []
        : deliveries.filter(
            (d) => d.triggerId === trigger.id || d.runs.some((r) => r.triggerId === trigger.id),
          ),
    [trigger, deliveries],
  );

  // ── Editing ───────────────────────────────────────────────────────────────
  // Every section keys its draft on the trigger's id, so opening a different
  // trigger cannot inherit half-typed edits from the last one.
  const editKey = trigger?.id ?? '';
  const editable = onEdit !== undefined && trigger !== null;

  const savePatch = useCallback(
    async (patch: TriggerEditPatch): Promise<string | null> => {
      if (trigger === null || onEdit === undefined) return 'Editing is not available here.';
      return onEdit(trigger, patch);
    },
    [trigger, onEdit],
  );

  const savePrompt = useCallback(
    (value: string) => savePatch({ promptTemplate: value }),
    [savePatch],
  );
  const prompt = useEditableSection(trigger?.promptTemplate ?? '', editKey, savePrompt);

  const saveName = useCallback((value: string) => savePatch({ name: value.trim() }), [savePatch]);
  const name = useEditableSection(trigger?.name ?? '', editKey, saveName);

  const storedFilters = useMemo(
    () => ({
      eventType: trigger?.eventType ?? '',
      filters: filtersFromMatchCondition(trigger?.matchCondition),
    }),
    [trigger?.eventType, trigger?.matchCondition],
  );
  const saveFilters = useCallback(
    (value: { eventType: string; filters: EditFilterRow[] }) =>
      // Sent together: the event narrows which fields exist, so a filter and
      // the event it belongs to must never land as two separate states.
      savePatch({
        eventType: value.eventType,
        matchCondition: matchConditionFrom(value.filters) ?? {},
      }),
    [savePatch],
  );
  const filters = useEditableSection(
    storedFilters,
    editKey,
    saveFilters,
    (a, b) => a.eventType === b.eventType && sameFilters(a.filters, b.filters),
  );

  const storedSchedule = useMemo(
    () => ({
      cronExpression: trigger?.scheduleConfig?.cronExpression ?? '',
      timezone: trigger?.scheduleConfig?.timezone ?? 'UTC',
      catchUp: trigger?.scheduleConfig?.catchUp ?? false,
    }),
    [trigger?.scheduleConfig],
  );
  const saveSchedule = useCallback(
    (value: { cronExpression: string; timezone: string; catchUp: boolean }) =>
      savePatch({ scheduleConfig: value }),
    [savePatch],
  );
  const schedule = useEditableSection(
    storedSchedule,
    editKey,
    saveSchedule,
    (a, b) =>
      a.cronExpression === b.cronExpression && a.timezone === b.timezone && a.catchUp === b.catchUp,
  );
  const [scheduleOpen, setScheduleOpen] = useState(false);

  const zones = useMemo(() => {
    const base = timezoneOptions(resolveBrowserZone());
    const current = storedSchedule.timezone;
    return current && !base.includes(current) ? [...base, current] : base;
  }, [storedSchedule.timezone]);

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

  const provider = source?.provider ?? '';
  // The catalog follows the event being edited, not the stored one, so the
  // fields on offer match the trigger the person is about to save.
  const promptCatalog =
    trigger.kind === 'webhook'
      ? toTemplateCatalog(
          webhookCatalog(provider, filters.value.eventType),
          catalogPatterns(provider),
        )
      : trigger.kind === 'scheduled'
        ? toTemplateCatalog(scheduledCatalog())
        : toTemplateCatalog(manualInputsCatalog(trigger.manualInputs));

  // A half-written filter looks configured but matches nothing, so it is the
  // one edit worth refusing rather than saving as-is.
  const filterBlocker = filters.value.filters.some(
    (row) => Boolean(row.field.trim()) !== splitFilterValues(row.values).length > 0,
  )
    ? 'Finish or remove the empty filter'
    : undefined;
  const nameBlocker = name.value.trim() ? undefined : 'Needs a name';
  const cronBlocker = isValidCron(schedule.value.cronExpression)
    ? undefined
    : 'Needs a five-field cron expression';

  const setFilterRow = (index: number, next: Partial<EditFilterRow>) =>
    filters.setValue({
      ...filters.value,
      filters: filters.value.filters.map((row, i) => (i === index ? { ...row, ...next } : row)),
    });

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
                      rotatedSecret !== null ? (
                        <CopyButton value={rotatedSecret} label="Copy" />
                      ) : (
                        <Button
                          variant="ghost"
                          size="xs"
                          disabled={onRotateSecret === undefined || rotating}
                          onClick={() => {
                            if (!rotateArmed) {
                              setRotateArmed(true);
                              return;
                            }
                            setRotating(true);
                            void onRotateSecret?.(trigger).then((secret) => {
                              setRotating(false);
                              setRotateArmed(false);
                              if (secret !== null) setRotatedSecret(secret);
                            });
                          }}
                        >
                          {rotating
                            ? 'Rotating…'
                            : rotateArmed
                              ? 'Break the old secret?'
                              : 'Rotate'}
                        </Button>
                      )
                    }
                  >
                    <Text mono size="caption" color={rotatedSecret !== null ? undefined : 'muted'}>
                      {/* The stored secret is write-once and never returned on
                          reads; only a rotation mints something to show. */}
                      {rotatedSecret ?? 'shown once at creation'}
                    </Text>
                  </FieldRow>
                  {rotatedSecret !== null && (
                    <Text size="caption" color="subtle">
                      Update your provider with this secret now. It is shown once and cannot be
                      recovered.
                    </Text>
                  )}
                  {/* Requests that fail authentication never become deliveries;
                      this counter is the only place a wrong secret shows up. */}
                  {source !== undefined && source.rejectionCount > 0 && (
                    <Text size="caption" color="status-error">
                      {source.rejectionCount} rejected
                      {source.lastRejectionReason !== null ? `: ${source.lastRejectionReason}` : ''}
                    </Text>
                  )}

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
                  {scheduleOpen ? (
                    <Stack gap="snug">
                      <Input
                        mono
                        aria-label="Cron expression"
                        value={schedule.value.cronExpression}
                        onChange={(e) =>
                          schedule.setValue({ ...schedule.value, cronExpression: e.target.value })
                        }
                        placeholder="0 2 * * *"
                      />
                      <Text size="caption" color="muted">
                        {describeCron(schedule.value.cronExpression)}
                      </Text>
                      <NativeSelect
                        mono
                        aria-label="Timezone"
                        value={schedule.value.timezone}
                        onChange={(e) =>
                          schedule.setValue({ ...schedule.value, timezone: e.target.value })
                        }
                      >
                        {zones.map((zone) => (
                          <option key={zone} value={zone}>
                            {zone}
                          </option>
                        ))}
                      </NativeSelect>
                      <ToggleRow
                        checked={schedule.value.catchUp}
                        onChange={(next) => schedule.setValue({ ...schedule.value, catchUp: next })}
                        label="Catch up on missed runs"
                        description="Replay ticks skipped while the platform was unavailable."
                      />
                      <EditActions
                        dirty={schedule.dirty}
                        pending={schedule.pending}
                        error={schedule.error}
                        blocker={cronBlocker}
                        saveLabel="Save schedule"
                        onSave={schedule.save}
                        onDiscard={() => {
                          schedule.discard();
                          setScheduleOpen(false);
                        }}
                      />
                      {!schedule.dirty && (
                        <Stack direction="horizontal" justify="end">
                          <Button variant="ghost" size="xs" onClick={() => setScheduleOpen(false)}>
                            Done
                          </Button>
                        </Stack>
                      )}
                    </Stack>
                  ) : (
                    <FieldRow
                      leading={<Icon name="clock" size="2xs" color="accent" />}
                      actions={
                        <Stack direction="horizontal" align="center" gap="snug">
                          <Text mono size="micro" color="subtle">
                            {trigger.scheduleConfig.timezone}
                          </Text>
                          {editable && (
                            <Button variant="ghost" size="xs" onClick={() => setScheduleOpen(true)}>
                              Edit
                            </Button>
                          )}
                        </Stack>
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
                  )}
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
                  <SegmentedControl
                    aria-label="Event type"
                    value={filters.value.eventType}
                    onChange={(v) => filters.setValue({ ...filters.value, eventType: v })}
                    options={[
                      { value: '', label: 'Any event' },
                      ...eventOptionsFor(provider, trigger.eventType).map((ev) => ({
                        value: ev,
                        label: ev,
                      })),
                    ]}
                  />

                  {filters.value.filters.map((row, i) => (
                    <Stack key={i} direction="horizontal" gap="snug" align="center">
                      <Stack minWidth="form-field-sm">
                        <NativeSelect
                          mono
                          aria-label={`Filter ${i + 1} field`}
                          value={row.field}
                          onChange={(e) => setFilterRow(i, { field: e.target.value })}
                        >
                          <option value="">Choose a field</option>
                          {webhookCatalog(provider, filters.value.eventType).map((entry) => (
                            <option key={entry.token} value={entry.token}>
                              {entry.token}
                            </option>
                          ))}
                          {/* A stored filter may name a field outside the
                              current event's catalog; keep it selectable so
                              editing the row cannot silently drop it. */}
                          {row.field !== '' &&
                            !webhookCatalog(provider, filters.value.eventType).some(
                              (entry) => entry.token === row.field,
                            ) && <option value={row.field}>{row.field}</option>}
                        </NativeSelect>
                      </Stack>
                      <Stack grow minWidth="none">
                        <Input
                          mono
                          aria-label={`Filter ${i + 1} values`}
                          value={row.values}
                          onChange={(e) => setFilterRow(i, { values: e.target.value })}
                          placeholder="opened | synchronize"
                        />
                      </Stack>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Remove filter ${i + 1}`}
                        onClick={() =>
                          filters.setValue({
                            ...filters.value,
                            filters: filters.value.filters.filter((_, x) => x !== i),
                          })
                        }
                      >
                        <Icon name="close" size="2xs" />
                      </Button>
                    </Stack>
                  ))}

                  {editable && (
                    <Stack direction="horizontal">
                      <Button
                        variant="ghost"
                        size="xs"
                        onClick={() =>
                          filters.setValue({
                            ...filters.value,
                            filters: [...filters.value.filters, { field: '', values: '' }],
                          })
                        }
                      >
                        <Icon name="plus" size="2xs" />
                        Add filter
                      </Button>
                    </Stack>
                  )}

                  <Text size="caption" color="subtle">
                    {describeMatchCondition(filters.value.filters) ??
                      'Every delivery of this event starts a run.'}
                  </Text>

                  <EditActions
                    dirty={filters.dirty}
                    pending={filters.pending}
                    error={filters.error}
                    blocker={filterBlocker}
                    saveLabel="Save filters"
                    onSave={filters.save}
                    onDiscard={filters.discard}
                  />
                </Stack>
              )}

              {/* Prompt: what the run is actually asked to do. */}
              <Stack gap="snug">
                <SectionLabel>Prompt</SectionLabel>
                <TemplateField
                  aria-label="Prompt template"
                  value={prompt.value}
                  onChange={prompt.setValue}
                  catalog={promptCatalog}
                  disabled={!editable}
                  placeholder="What this trigger asks the agent to do."
                  catalogLabel={
                    trigger.kind === 'webhook' && filters.value.eventType
                      ? `Fields for ${filters.value.eventType} deliveries`
                      : undefined
                  }
                />
                <EditActions
                  dirty={prompt.dirty}
                  pending={prompt.pending}
                  error={prompt.error}
                  saveLabel="Save prompt"
                  onSave={prompt.save}
                  onDiscard={prompt.discard}
                />
              </Stack>

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
                    onRedeliver={onRedeliver}
                  />
                </>
              )}
            </Stack>
          </TabsContent>

          {/* ── Settings: how it should behave ──────────────────────────── */}
          <TabsContent value="settings">
            <Stack gap="comfortable">
              <Stack gap="snug">
                <SectionLabel>Identity</SectionLabel>
                <SettingRow
                  label="Name"
                  description="Shown in the triggers list and on every run it starts."
                  value={
                    <Stack minWidth="form-field-sm">
                      <Input
                        mono
                        compact
                        aria-label="Trigger name"
                        value={name.value}
                        disabled={!editable}
                        onChange={(e) => name.setValue(e.target.value)}
                        maxLength={128}
                      />
                    </Stack>
                  }
                />
                <EditActions
                  dirty={name.dirty}
                  pending={name.pending}
                  error={name.error}
                  blocker={nameBlocker}
                  saveLabel="Save name"
                  onSave={name.save}
                  onDiscard={name.discard}
                />
              </Stack>

              <Stack gap="snug">
                <SectionLabel>Delivery</SectionLabel>
                <Stack gap="snug">
                  <SettingRow
                    label="Runs at once"
                    description="How many runs this trigger may have in flight together."
                    value={
                      <StepperControl
                        value={trigger.maxParallel}
                        min={1}
                        max={16}
                        label="runs at once"
                        disabled={onUpdateSafety === undefined}
                        onChange={(next) => onUpdateSafety?.(trigger, { maxParallel: next })}
                      />
                    }
                  />
                  <SettingRow
                    label="At the limit"
                    description="What happens to an arrival that cannot start yet."
                    value={
                      <SegmentedControl
                        aria-label="Overflow behaviour"
                        value={trigger.overflow}
                        onChange={(v) =>
                          onUpdateSafety?.(trigger, { overflow: v as TriggerRow['overflow'] })
                        }
                        options={[
                          { value: 'wait', label: 'Wait' },
                          { value: 'replace', label: 'Replace' },
                          { value: 'refuse', label: 'Refuse' },
                        ]}
                      />
                    }
                  />
                  {trigger.overflow === 'wait' && (
                    <SettingRow
                      label="Queue depth"
                      description="How many deliveries may wait before new ones are refused."
                      value={
                        <StepperControl
                          value={trigger.maxQueueDepth}
                          min={1}
                          max={500}
                          step={trigger.maxQueueDepth >= 50 ? 25 : 1}
                          label="queue depth"
                          disabled={onUpdateSafety === undefined}
                          onChange={(next) => onUpdateSafety?.(trigger, { maxQueueDepth: next })}
                        />
                      }
                    />
                  )}
                  <SettingRow
                    label="Start timeout"
                    description="How long a queued delivery may wait before it is dropped."
                    value={
                      <StepperControl
                        value={trigger.timeoutSeconds}
                        min={5}
                        max={600}
                        step={5}
                        suffix="s"
                        label="start timeout"
                        disabled={onUpdateSafety === undefined}
                        onChange={(next) => onUpdateSafety?.(trigger, { timeoutSeconds: next })}
                      />
                    }
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
