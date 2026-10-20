'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the drawer is the deepest trigger-domain surface
 * there is, and promoting it would carry the whole vocabulary into
 * `src/components/`.
 *
 * Everything about one trigger, in one place.
 */

import { useCallback, useId, useMemo, useState } from 'react';
import {
  CopyButton,
  Alert,
  Button,
  Card,
  CardContent,
  DangerZone,
  FieldRow,
  FormField,
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
  EmptyState,
} from '@runkora/hades';
import { KIND_ICON, LIMITS } from '../_types';
import type { DeliveryRow, Senders, SourceRow, TriggerCoalesce, TriggerRow } from '../_types';
import { isStartedOutcome, recentFailures } from '../_types/fleet';
import { endpointFor } from '../_lib/endpoints';
import { SLACK_PROVIDER } from '../_lib/slack-app-manifest';
import { CatalogPicker } from './catalog-picker';
import { CoalesceEditor } from './coalesce-editor';
import type { CoalesceDraft } from './coalesce-editor';
import { DataBlockPreview } from './data-block-preview';
import { DeliveryList } from './delivery-list';
import type { RoutingAgentName } from './delivery-routing';
import { RoutedTarget } from './routed-target';
import { FilterRowsEditor } from './filter-rows-editor';
import type { FilterRow } from './filter-rows-editor';
import { SenderRestrictionEditor } from './sender-restriction-editor';
import type { SenderRestrictionDraft } from './sender-restriction-editor';
import { useSourceCatalog } from './use-source-catalog';
import {
  eventTypeCatalog,
  schemaCatalog,
  scheduledCatalog,
  toTemplateCatalog,
} from './trigger-wizard/field-catalog';
import {
  describeMatchCondition,
  matchConditionFrom,
  coalesceFrom,
  matchContainsFrom,
  splitLogins,
  validateCoalesce,
  validateFilters,
} from './trigger-wizard/wizard-state';
import {
  describeCron,
  isValidCron,
  resolveBrowserZone,
  timezoneOptions,
} from './trigger-wizard/cron';
import { filtersFromMatchCondition, manualInputsCatalog, sameFilters } from './trigger-edit';
import { useEditableSection } from './use-editable-section';
import { useHasScope } from '../../_components/permissions-provider';
import { PermissionHint, permissionHintText } from '@runkora/components';

/**
 * A correction to a stored trigger. Every field is optional: a section saves
 * only what it owns, so renaming a trigger cannot disturb its filters.
 */
export interface TriggerEditPatch {
  name?: string;
  promptTemplate?: string;
  eventType?: string;
  matchCondition?: Record<string, string[]>;
  matchContains?: Record<string, string[]>;
  /** Replaces the restriction; both lists empty removes it. */
  senderRestriction?: Senders;
  /** Replaces the setting; null removes it. */
  coalesce?: TriggerCoalesce | null;
  scheduleConfig?: { cronExpression: string; timezone: string; catchUp: boolean };
}

/** Overflow choices in the drawer. Wait is first: it is the only one coalescing allows. */
const OVERFLOW_OPTIONS = [
  { value: 'wait', label: 'Wait' },
  { value: 'replace', label: 'Replace' },
  { value: 'refuse', label: 'Refuse' },
];

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
   * Shown disabled with the reason when the caller may not replay.
   */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
  /**
   * Rotates the webhook source's signing secret and resolves with the new one,
   * which is shown once here and never again, or null on failure. For a
   * provider that issues the secret, it stores `signingSecret` and resolves
   * with an empty string. Absent for read-only callers.
   */
  onRotateSecret?: (t: TriggerRow, signingSecret?: string) => Promise<string | null>;
  /** Fire a manual trigger now. Absent for kinds that fire on their own. */
  onFire?: (t: TriggerRow) => void;
  firing?: boolean;
  /**
   * Whether the caller may run the bound agent, or every candidate of a
   * routed trigger. Changing what a trigger runs or when (prompt, filters,
   * senders, schedule, routing, on or off, redeliver, secret rotation) needs it
   * on top of triggers:write.
   */
  canRunAgent: boolean;
  /** Names a routed trigger's candidates and the agents its decisions mention. */
  agents?: RoutingAgentName[];
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

/**
 * The secret row for a source whose provider issues the signing secret. The
 * admin regenerates it in Slack and pastes the new value here; nothing is
 * shown back.
 */
function IssuedSecretRow({ onReplace }: { onReplace?: (secret: string) => Promise<boolean> }) {
  const id = useId();
  const [value, setValue] = useState('');
  const [pending, setPending] = useState(false);
  const [replaced, setReplaced] = useState(false);

  const save = () => {
    if (onReplace === undefined) return;
    setPending(true);
    void onReplace(value.trim()).then((ok) => {
      setPending(false);
      if (!ok) return;
      setValue('');
      setReplaced(true);
    });
  };

  return (
    <Stack gap="snug">
      <FormField
        label="Signing secret"
        id={id}
        hint="Regenerate it in your Slack app, then paste the new secret here."
      >
        <Input
          id={id}
          type="password"
          autoComplete="off"
          mono
          value={value}
          disabled={onReplace === undefined || pending}
          onChange={(e) => setValue(e.target.value)}
          placeholder="Stored. Paste a new secret to replace it."
          maxLength={256}
        />
      </FormField>
      {replaced && (
        <Text size="caption" color="subtle">
          The new secret is stored. Requests signed with the old one are rejected.
        </Text>
      )}
      <EditActions
        dirty={value.trim() !== ''}
        pending={pending}
        error={null}
        saveLabel="Replace secret"
        onSave={save}
        onDiscard={() => setValue('')}
      />
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
  onUpdateSafety: updateSafetyAction,
  onEdit: editAction,
  onRedeliver: redeliverAction,
  onRotateSecret: rotateSecretAction,
  onFire: fireAction,
  firing = false,
  canRunAgent,
  agents = [],
}: TriggerDrawerProps) {
  // Without triggers:write every write handler is dropped, which disables its
  // control. Run-shaping edits also need run authority on the bound agent.
  // Firing has its own per-trigger answer from the API.
  const canWrite = useHasScope('triggers:write');
  const canShapeRuns = canWrite && canRunAgent;
  const onUpdateSafety = canWrite ? updateSafetyAction : undefined;
  const onEdit = canWrite ? editAction : undefined;
  const onRotateSecret = canShapeRuns ? rotateSecretAction : undefined;
  const canFire = !!trigger?.canFire;
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
  const runEditable = editable && canRunAgent;

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
  const name = useEditableSection(trigger?.name ?? '', editKey, saveName, {
    validate: (value) => (value.trim() ? undefined : 'Needs a name'),
  });

  const storedFilters = useMemo(
    () => ({
      eventType: trigger?.eventType ?? '',
      filters: filtersFromMatchCondition(trigger?.matchCondition, trigger?.matchContains),
    }),
    [trigger?.eventType, trigger?.matchCondition, trigger?.matchContains],
  );
  const saveFilters = useCallback(
    (value: { eventType: string; filters: FilterRow[] }) =>
      // Sent together: the event narrows which fields exist, so a filter and
      // the event it belongs to must never land as two separate states.
      savePatch({
        eventType: value.eventType,
        matchCondition: matchConditionFrom(value.filters) ?? {},
        matchContains: matchContainsFrom(value.filters) ?? {},
      }),
    [savePatch],
  );
  const filters = useEditableSection(storedFilters, editKey, saveFilters, {
    isEqual: (a, b) => a.eventType === b.eventType && sameFilters(a.filters, b.filters),
    // A half-written filter looks configured but matches nothing, so it is the
    // one edit worth refusing rather than saving as-is. Same rule as the
    // wizard's, from the same function.
    validate: (value) => validateFilters(value.filters).hint,
  });

  const storedSenders = useMemo(
    () => ({
      logins: (trigger?.senderRestriction.logins ?? []).join(', '),
      associations: trigger?.senderRestriction.associations ?? [],
    }),
    [trigger?.senderRestriction],
  );
  const saveSenders = useCallback(
    (value: SenderRestrictionDraft) =>
      savePatch({
        senderRestriction: { logins: splitLogins(value.logins), associations: value.associations },
      }),
    [savePatch],
  );
  const senders = useEditableSection(storedSenders, editKey, saveSenders, {
    isEqual: (a, b) =>
      a.logins === b.logins &&
      [...a.associations].sort().join() === [...b.associations].sort().join(),
  });

  const storedCoalesce = useMemo<CoalesceDraft>(
    () => ({
      enabled: trigger?.coalesce != null,
      key: trigger?.coalesce?.key ?? '',
      window: trigger?.coalesce ? String(trigger.coalesce.windowSeconds) : '',
    }),
    [trigger?.coalesce],
  );
  const saveCoalesce = useCallback(
    // Off clears the setting; on was validated before save is offered.
    (value: CoalesceDraft) => savePatch({ coalesce: coalesceFrom(value) ?? null }),
    [savePatch],
  );
  const coalesce = useEditableSection(storedCoalesce, editKey, saveCoalesce, {
    isEqual: (a, b) =>
      a.enabled === b.enabled &&
      (!a.enabled || (a.key.trim() === b.key.trim() && a.window.trim() === b.window.trim())),
    validate: (value) => validateCoalesce(value),
  });

  // The catalogue follows the event being edited.
  const webhookSource =
    trigger?.kind === 'webhook' ? sources.find((s) => s.id === trigger.sourceId) : undefined;
  const sourceCatalog = useSourceCatalog(
    webhookSource?.provider ?? '',
    webhookSource?.id ?? '',
    filters.value.eventType,
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
  const schedule = useEditableSection(storedSchedule, editKey, saveSchedule, {
    isEqual: (a, b) =>
      a.cronExpression === b.cronExpression && a.timezone === b.timezone && a.catchUp === b.catchUp,
    validate: (value) =>
      isValidCron(value.cronExpression) ? undefined : 'Needs a five-field cron expression',
  });
  const [scheduleOpen, setScheduleOpen] = useState(false);

  // The edit sections above key their drafts on editKey; this ephemeral UI
  // state does not, and the drawer instance outlives a close (both pages render
  // it unconditionally and it only hides itself when trigger is null). Reset it
  // during render when the trigger changes, the way the lists reset paging, so
  // a reopened drawer never shows the last trigger's revealed secret, armed
  // rotation, open schedule, or expanded delivery.
  const [stateKey, setStateKey] = useState(editKey);
  if (stateKey !== editKey) {
    setStateKey(editKey);
    setExpanded(null);
    setRotateArmed(false);
    setRotating(false);
    setRotatedSecret(null);
    setScheduleOpen(false);
  }

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
      n + d.runs.filter((r) => r.triggerId === trigger.id && isStartedOutcome(r.status)).length,
    0,
  );
  const shown = mine.slice(0, DELIVERY_SAMPLE);
  const endpoint = endpointFor(ingestHost, trigger.sourceId);

  const webhookFields = schemaCatalog(sourceCatalog.fields);
  const promptCatalog =
    trigger.kind === 'webhook'
      ? webhookFields
      : trigger.kind === 'scheduled'
        ? toTemplateCatalog(scheduledCatalog())
        : toTemplateCatalog(manualInputsCatalog(trigger.manualInputs));

  const setFilterRows = (rows: FilterRow[]) =>
    filters.setValue({ ...filters.value, filters: rows });

  const subtitle =
    trigger.kind === 'webhook'
      ? `Webhook · ${source?.name ?? trigger.sourceId} · ${trigger.eventType || 'no event type (never fires)'}`
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
            disabled={!canShapeRuns}
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
          {/* One notice covers every disabled edit control in the drawer. */}
          {!canWrite ? (
            <Alert variant="info">{permissionHintText('edit triggers')}</Alert>
          ) : (
            !canRunAgent && (
              <Alert variant="info">
                {permissionHintText("run this trigger's agent")} Its prompt, filters, schedule and
                on or off state are read only.
              </Alert>
            )
          )}
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

                  {source?.provider === SLACK_PROVIDER ? (
                    <IssuedSecretRow
                      key={trigger.id}
                      onReplace={
                        onRotateSecret &&
                        ((secret) => onRotateSecret(trigger, secret).then((r) => r !== null))
                      }
                    />
                  ) : (
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
                      <Text
                        mono
                        size="caption"
                        color={rotatedSecret !== null ? undefined : 'muted'}
                      >
                        {/* The stored secret is write-once and never returned on
                          reads; only a rotation mints something to show. */}
                        {rotatedSecret ?? 'shown once at creation'}
                      </Text>
                    </FieldRow>
                  )}
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
                        blocker={schedule.blocker}
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
                          {editAction !== undefined && (
                            <Button
                              variant="ghost"
                              size="xs"
                              disabled={!runEditable}
                              onClick={() => setScheduleOpen(true)}
                            >
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
                        POST /api/v1/trigger-fires/{trigger.id}
                      </Text>
                      {fireAction !== undefined && (
                        <PermissionHint allowed={canFire} action="fire this trigger">
                          <Button
                            size="sm"
                            disabled={!canFire || !trigger.enabled || firing}
                            onClick={() => fireAction(trigger)}
                          >
                            {firing ? 'Queueing…' : 'Run now'}
                          </Button>
                        </PermissionHint>
                      )}
                    </Stack>
                  </Card>
                </Stack>
              )}

              {/* Event filters: what this trigger accepts of what arrives. */}
              {trigger.kind === 'webhook' && (
                <Stack gap="snug">
                  <SectionLabel>Event filters</SectionLabel>
                  <CatalogPicker
                    aria-label="Event type"
                    value={filters.value.eventType}
                    placeholder="Choose an event"
                    catalog={eventTypeCatalog(sourceCatalog.declared, sourceCatalog.observed)}
                    searchPlaceholder="Search events…"
                    disabled={!runEditable}
                    onSelect={(eventType) => filters.setValue({ ...filters.value, eventType })}
                  />

                  <FilterRowsEditor
                    rows={filters.value.filters}
                    fields={webhookFields}
                    onChange={setFilterRows}
                    disabled={!runEditable}
                  />

                  <Text size="caption" color="subtle">
                    {describeMatchCondition(filters.value.filters) ??
                      'Every delivery of this event starts a run.'}
                  </Text>

                  <EditActions
                    dirty={filters.dirty}
                    pending={filters.pending}
                    error={filters.error}
                    blocker={filters.blocker}
                    saveLabel="Save filters"
                    onSave={filters.save}
                    onDiscard={filters.discard}
                  />
                </Stack>
              )}

              {/* Senders: who may fire it, when the provider says who caused an event. */}
              {trigger.kind === 'webhook' && (
                <SenderRestrictionEditor
                  identifiesSenders={sourceCatalog.identifiesSenders}
                  available={sourceCatalog.associations}
                  value={senders.value}
                  onChange={senders.setValue}
                  disabled={!runEditable}
                  actions={
                    <EditActions
                      dirty={senders.dirty}
                      pending={senders.pending}
                      error={senders.error}
                      saveLabel="Save senders"
                      onSave={senders.save}
                      onDiscard={senders.discard}
                    />
                  }
                />
              )}

              {/* Coalescing: collapse a burst of similar fires into one run. */}
              {trigger.kind === 'webhook' && (
                <CoalesceEditor
                  value={coalesce.value}
                  onChange={coalesce.setValue}
                  disabled={!editable || trigger.overflow !== 'wait'}
                  note={trigger.overflow !== 'wait' ? 'Requires overflow set to wait.' : undefined}
                  actions={
                    <EditActions
                      dirty={coalesce.dirty}
                      pending={coalesce.pending}
                      error={coalesce.error}
                      blocker={coalesce.blocker}
                      saveLabel="Save coalescing"
                      onSave={coalesce.save}
                      onDiscard={coalesce.discard}
                    />
                  }
                />
              )}

              {/* Prompt: what the run is actually asked to do. */}
              <Stack gap="snug">
                <SectionLabel>Prompt</SectionLabel>
                <TemplateField
                  aria-label="Prompt template"
                  value={prompt.value}
                  onChange={prompt.setValue}
                  catalog={promptCatalog}
                  disabled={!runEditable}
                  placeholder="What this trigger asks the agent to do."
                  catalogLabel={
                    trigger.kind === 'webhook' && filters.value.eventType
                      ? `Fields for ${filters.value.eventType} deliveries`
                      : undefined
                  }
                />
                {trigger.kind === 'webhook' && <DataBlockPreview template={prompt.value} />}
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
                {trigger.routing !== null ? (
                  <RoutedTarget
                    routing={trigger.routing}
                    agents={agents}
                    limits={`${trigger.maxParallel} parallel · ${trigger.overflow}`}
                  />
                ) : (
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
                )}
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
                    onRedeliver={redeliverAction}
                    redeliverAllowed={canShapeRuns}
                    agents={agents}
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
                  blocker={name.blocker}
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
                        min={LIMITS.parallel.min}
                        max={LIMITS.parallel.max}
                        label="runs at once"
                        disabled={onUpdateSafety === undefined}
                        onChange={(next) => onUpdateSafety?.(trigger, { maxParallel: next })}
                      />
                    }
                  />
                  <SettingRow
                    label="At the limit"
                    description={
                      trigger.coalesce
                        ? 'Coalescing requires Wait. Turn coalescing off to change this.'
                        : 'What happens to an arrival that cannot start yet.'
                    }
                    value={
                      <SegmentedControl
                        aria-label="Overflow behaviour"
                        value={trigger.overflow}
                        onChange={(v) =>
                          onUpdateSafety?.(trigger, { overflow: v as TriggerRow['overflow'] })
                        }
                        options={trigger.coalesce ? OVERFLOW_OPTIONS.slice(0, 1) : OVERFLOW_OPTIONS}
                      />
                    }
                  />
                  {trigger.overflow === 'wait' && (
                    <SettingRow
                      label="Queue depth"
                      description="How many deliveries may wait. Beyond this the oldest waiting delivery is dropped."
                      value={
                        <StepperControl
                          value={trigger.maxQueueDepth}
                          min={LIMITS.queueDepth.min}
                          max={LIMITS.queueDepth.max}
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
                    description="Bounds the start request, which returns once the run is queued. Dispatch is abandoned after this window."
                    value={
                      <StepperControl
                        value={trigger.timeoutSeconds}
                        min={LIMITS.timeout.min}
                        max={LIMITS.timeout.max}
                        step={5}
                        suffix="s"
                        label="start timeout"
                        disabled={onUpdateSafety === undefined}
                        onChange={(next) => onUpdateSafety?.(trigger, { timeoutSeconds: next })}
                      />
                    }
                  />
                  {/* Only when something is actually waiting: a permanent
                      "0 waiting" would read as a queue that exists rather than
                      as a trigger keeping up. */}
                  {trigger.pendingCount > 0 && (
                    <SettingRow
                      label="Waiting now"
                      description="Deliveries admitted and queued, not yet started."
                      value={
                        <Text mono size="supporting" weight="heavy">
                          {trigger.pendingCount}
                        </Text>
                      }
                    />
                  )}
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
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={!canWrite}
                    onClick={() => onDelete(trigger)}
                  >
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
