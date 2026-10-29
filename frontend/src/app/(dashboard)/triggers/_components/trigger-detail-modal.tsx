'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the modal is the deepest trigger-domain surface
 * there is, and promoting it would carry the whole vocabulary into
 * `src/components/`.
 *
 * Everything about one trigger, in one place.
 */

import Link from 'next/link';
import { useCallback, useId, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import {
  CopyButton,
  CopyField,
  Alert,
  Badge,
  Button,
  Card,
  DangerZone,
  CardContent,
  ConfirmDialog,
  Heading,
  Grid,
  Icon,
  IconTile,
  Input,
  SectionLabel,
  Modal,
  ModalHeader,
  Stack,
  StatusDot,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  TemplateField,
  Wizard,
  Text,
  Toggle,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { consoleKindOf, triggerToggleOpen } from '../_types';
import type { DeliveryRow, Senders, SourceRow, TriggerCoalesce, TriggerRow } from '../_types';
import { isStartedOutcome, recentFailures } from '../_types/fleet';
import { endpointFor } from '../_lib/endpoints';
import { SLACK_PROVIDER } from '../_lib/slack-app-manifest';
import { chatConversationKey, chatEventLabel } from '../_lib/chat';
import { providerName, SENDER_ID_EXAMPLE } from '../_lib/providers';
import { takesPastedSecret, verificationMethodOf, verificationNote } from '../_lib/generic-source';
import { ConfigurationRail } from './configuration-rail';
import type { ConfigurationSection } from './configuration-rail';
import type { CoalesceDraft } from './coalesce-editor';
import { DataBlockPreview } from './data-block-preview';
import { DeliveryList } from './delivery-list';
import type { RoutingAgentName } from './delivery-routing';
import { PlainRow } from './plain-row';
import { RoutedTarget } from './routed-target';
import { DeliverySection, deliveryBlocker, sameDelivery } from './delivery-section';
import type { DeliveryValue } from './delivery-section';
import { EventFiltersSection } from './event-filters-section';
import { ScheduleBuilder, useScheduleDraft } from './schedule-builder';
import { SectionField } from './section-mode';
import { TriggerNameField } from './trigger-fields';
import type { FilterRow } from './filter-rows-editor';
import { SenderRestrictionEditor } from './sender-restriction-editor';
import type { SenderRestrictionDraft } from './sender-restriction-editor';
import { useSourceCatalog } from './use-source-catalog';
import { schemaCatalog, scheduledCatalog, toTemplateCatalog } from './trigger-wizard/field-catalog';
import {
  matchConditionFrom,
  coalesceFrom,
  matchContainsFrom,
  splitIds,
  validateFilters,
} from './trigger-wizard/wizard-state';
import {
  cronBreakdown,
  describeCron,
  resolveBrowserZone,
  timezoneOptions,
} from './trigger-wizard/cron';
import { filtersFromMatchCondition, manualInputsCatalog, sameFilters } from './trigger-edit';
import { ManagedSectionNotice, managedFooterMessage } from './managed-section-notice';
import { useEditableSection } from './use-editable-section';
import { useHasScope, useTenantLocked } from '../../_components/permissions-provider';
import {
  ManagedInGitBadge,
  ManagedInGitNotice,
  PermissionHint,
  permissionHintText,
} from '@runkora/components';
import { managedInGit } from '@runkora/lib/declarative';

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
  maxParallel?: number;
  overflow?: TriggerRow['overflow'];
  maxQueueDepth?: number;
  timeoutSeconds?: number;
  dedupeEnabled?: boolean;
  autopauseEnabled?: boolean;
}

interface TriggerDetailModalProps {
  trigger: TriggerRow | null;
  sources: SourceRow[];
  deliveries: DeliveryRow[];
  /** Public base URL webhook senders post to. */
  ingestHost: string;
  onClose: () => void;
  onToggleEnabled: (t: TriggerRow, enabled: boolean) => void;
  onDelete: (t: TriggerRow) => void;
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
   * with an empty string. `firstSecret` sets the first secret of a source that
   * is waiting for one, without rotating. Absent for read-only callers.
   */
  onRotateSecret?: (
    t: TriggerRow,
    signingSecret?: string,
    firstSecret?: boolean,
  ) => Promise<string | null>;
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
  notice,
  alwaysShow = false,
  readOnly = false,
  saveLabel,
  onSave,
  onDiscard,
}: {
  dirty: boolean;
  pending: boolean;
  error: string | null;
  /** Why this cannot be saved yet, if anything. */
  blocker?: string;
  /** A quiet line beside the buttons, e.g. why they are dead. A blocker takes its place. */
  notice?: string;
  /** Keep the buttons on screen, disabled, while nothing has changed. */
  alwaysShow?: boolean;
  /** Nothing can be changed: the buttons stay, and the notice says why. */
  readOnly?: boolean;
  saveLabel: string;
  onSave: () => void;
  onDiscard: () => void;
}) {
  if (!dirty && !alwaysShow) return null;
  return (
    <Stack gap="snug">
      {error !== null && <Alert variant="error">{error}</Alert>}
      <Stack direction="horizontal" align="center" gap="snug">
        <Stack grow minWidth="none">
          {blocker !== undefined ? (
            <Text size="caption" color="status-error">
              {blocker}
            </Text>
          ) : (
            notice !== undefined && (
              <Text size="caption" color="muted">
                {notice}
              </Text>
            )
          )}
        </Stack>
        <Button
          variant="ghost"
          size="xs"
          disabled={readOnly || !dirty || pending}
          onClick={onDiscard}
        >
          Discard
        </Button>
        <Button
          size="xs"
          disabled={readOnly || !dirty || pending || blocker !== undefined}
          onClick={onSave}
        >
          {pending ? 'Saving…' : saveLabel}
        </Button>
      </Stack>
    </Stack>
  );
}

/**
 * The secret for a source whose provider issues the signing secret. The admin
 * regenerates it in the provider's app and pastes the new value here; nothing
 * is shown back. It owns the draft but not the layout: the caller places the
 * field in its section and the save actions after the screen's last content.
 */
function IssuedSecret({
  waiting = false,
  placeholder,
  onReplace,
  children,
}: {
  /** The source has no secret yet: the first one is set, not replaced. */
  waiting?: boolean;
  /** Replaces the stored-secret placeholder, for a sender that may also be given a generated one. */
  placeholder?: string;
  onReplace?: (secret: string) => Promise<boolean>;
  children: (parts: { field: ReactNode; actions: ReactNode }) => ReactNode;
}) {
  const id = useId();
  const [value, setValue] = useState('');
  const [pending, setPending] = useState(false);
  // What the last save did, fixed at save time: the source stops waiting once it is saved.
  const [saved, setSaved] = useState<'first' | 'replaced' | null>(null);

  const save = () => {
    if (onReplace === undefined) return;
    setPending(true);
    void onReplace(value.trim()).then((ok) => {
      setPending(false);
      if (!ok) return;
      setValue('');
      setSaved(waiting ? 'first' : 'replaced');
    });
  };

  const field = (
    <Stack gap="snug">
      <Input
        id={id}
        aria-label="Signing secret"
        type="password"
        autoComplete="off"
        mono
        value={value}
        disabled={onReplace === undefined || pending}
        onChange={(e) => setValue(e.target.value)}
        placeholder={
          placeholder ??
          (waiting ? 'Paste the signing secret' : 'Stored. Paste a new secret to replace it.')
        }
        maxLength={256}
      />
      {saved !== null && (
        <Text size="caption" color="subtle">
          {saved === 'first'
            ? 'The signing secret is stored. The source now accepts events.'
            : 'The new secret is stored. Requests signed with the old one are rejected.'}
        </Text>
      )}
    </Stack>
  );
  const actions = (
    <EditActions
      dirty={value.trim() !== ''}
      pending={pending}
      error={null}
      saveLabel={waiting ? 'Set secret' : 'Replace secret'}
      onSave={save}
      onDiscard={() => setValue('')}
    />
  );
  return <>{children({ field, actions })}</>;
}

/** Why the signing secret matters: what is checked on every delivery, and what happens to failures. */
function SigningInfo({ note }: { note?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="How deliveries are verified">
          <Icon name="info" size="2xs" color="subtle" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        {note ??
          'Deliveries are verified with an HMAC signature over the raw body. Unsigned or stale requests are rejected before any run starts, and are counted on the source rather than recorded as deliveries.'}
      </TooltipContent>
    </Tooltip>
  );
}

/** A small figure in the modal's stat trio. */
function DetailStat({
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
    <Card subtle border="subtle">
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

function TriggerDetailModalBody({
  trigger,
  sources,
  deliveries,
  ingestHost,
  onClose,
  onToggleEnabled,
  onDelete,
  onEdit: editAction,
  onRedeliver: redeliverAction,
  onRotateSecret: rotateSecretAction,
  onFire: fireAction,
  firing = false,
  canRunAgent,
  agents = [],
}: TriggerDetailModalProps) {
  // Without triggers:write every write handler is dropped, which disables its
  // control. Run-shaping edits also need run authority on the bound agent.
  // Firing has its own per-trigger answer from the API.
  const canWrite = useHasScope('triggers:write');
  const canShapeRuns = canWrite && canRunAgent;
  // A trigger whose source was deleted takes no edits; it can only be turned off or deleted.
  const sourceMissing = trigger?.sourceMissing ?? false;
  // A trigger managed in git keeps manual fire and re-enabling after an auto-pause, and nothing else.
  const tenantLocked = useTenantLocked();
  const managed = trigger !== null && managedInGit(trigger.declared, tenantLocked);
  // A non-webhook trigger has no source id, so it finds no source.
  const source = trigger === null ? undefined : sources.find((s) => s.id === trigger.sourceId);
  // Secret rotation stays available on an API source, even in a locked tenant.
  const sourceDeclared = source?.declared ?? false;
  const onEdit = canWrite && !sourceMissing && !managed ? editAction : undefined;
  const onRotateSecret = canShapeRuns && !sourceDeclared ? rotateSecretAction : undefined;
  const canFire = !!trigger?.canFire;
  const [expanded, setExpanded] = useState<string | null>(null);
  // Rotation is destructive (the provider's configured secret stops working the
  // moment it lands), so the button opens a confirmation before doing it.
  const [rotateOpen, setRotateOpen] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);
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
        eventType: value.eventType.trim(),
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
      ids: (trigger?.senderRestriction.ids ?? []).join(', '),
      roles: trigger?.senderRestriction.roles ?? [],
    }),
    [trigger?.senderRestriction],
  );
  const saveSenders = useCallback(
    (value: SenderRestrictionDraft) =>
      savePatch({
        senderRestriction: { ids: splitIds(value.ids), roles: value.roles },
      }),
    [savePatch],
  );
  const senders = useEditableSection(storedSenders, editKey, saveSenders, {
    isEqual: (a, b) => a.ids === b.ids && [...a.roles].sort().join() === [...b.roles].sort().join(),
  });

  const storedCoalesce = useMemo<CoalesceDraft>(
    () => ({
      enabled: trigger?.coalesce != null,
      key: trigger?.coalesce?.key ?? '',
      window: trigger?.coalesce ? String(trigger.coalesce.windowSeconds) : '',
    }),
    [trigger?.coalesce],
  );
  const storedDelivery = useMemo<DeliveryValue>(
    () => ({
      maxParallel: trigger?.maxParallel ?? 1,
      overflow: trigger?.overflow ?? 'wait',
      maxQueueDepth: trigger?.maxQueueDepth ?? 32,
      timeoutSeconds: trigger?.timeoutSeconds ?? 30,
      dedupeEnabled: trigger?.dedupeEnabled ?? false,
      autopauseEnabled: trigger?.autopauseEnabled ?? false,
      coalesce: storedCoalesce,
    }),
    [trigger, storedCoalesce],
  );
  const saveDelivery = useCallback(
    (value: DeliveryValue) =>
      // One request for the whole section: the bounds are checked against each
      // other, and coalescing only exists under `wait`, so a split save could
      // be refused halfway.
      savePatch({
        maxParallel: value.maxParallel,
        overflow: value.overflow,
        // The queue only exists under `wait` and is validated only there, so a
        // value left over from a different mode is not sent for the backend to refuse.
        ...(value.overflow === 'wait' && { maxQueueDepth: value.maxQueueDepth }),
        timeoutSeconds: value.timeoutSeconds,
        dedupeEnabled: value.dedupeEnabled,
        autopauseEnabled: value.autopauseEnabled,
        ...(trigger?.kind === 'webhook' && {
          coalesce: (value.overflow === 'wait' ? coalesceFrom(value.coalesce) : undefined) ?? null,
        }),
      }),
    [savePatch, trigger?.kind],
  );
  const delivery = useEditableSection(storedDelivery, editKey, saveDelivery, {
    isEqual: sameDelivery,
    validate: deliveryBlocker,
  });

  // The catalogue follows the event being edited.
  const webhookSource = trigger?.kind === 'webhook' ? source : undefined;
  const sourceCatalog = useSourceCatalog(
    webhookSource?.provider ?? '',
    webhookSource?.id ?? '',
    filters.value.eventType,
  );
  // A generic source has no schema: its event type and filter fields are typed.
  const schemaless = trigger?.kind === 'webhook' && sourceCatalog.schemaless;
  // A typed event type can be emptied; a picked one cannot.
  const filtersBlocker =
    schemaless && !filters.value.eventType.trim() ? 'Needs an event type' : filters.blocker;
  const genericMethod =
    webhookSource === undefined
      ? undefined
      : verificationMethodOf(webhookSource.provider, webhookSource.config);

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
    validate: (value) => cronBreakdown(value.cronExpression).problem,
  });
  const [scheduleDraft, patchSchedule] = useScheduleDraft(schedule.value, schedule.setValue);

  // Drafts are dropped explicitly as well, so a close never leaves unsaved edits behind
  // for anything that holds a section past the modal's own unmount.
  const discardAndClose = () => {
    for (const section of [name, schedule, filters, senders, prompt, delivery]) section.discard();
    setConfirmingDiscard(false);
    onClose();
  };
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  // Escape, the overlay and × all land here; an edit is never thrown away without asking.
  const handleClose = () => {
    const dirty = [name, schedule, filters, senders, prompt, delivery].some((x) => x.dirty);
    if (dirty) setConfirmingDiscard(true);
    else discardAndClose();
  };

  const zones = useMemo(() => {
    const base = timezoneOptions(resolveBrowserZone());
    const current = storedSchedule.timezone;
    return current && !base.includes(current) ? [...base, current] : base;
  }, [storedSchedule.timezone]);

  const [tab, setTab] = useState('overview');
  const [section, setSection] = useState('');

  if (trigger === null) return null;

  // A chat command is stored as a webhook trigger; its source's category tells them apart.
  const kind = consoleKindOf(trigger, (id) => sources.find((s) => s.id === id)?.category);
  const eventName =
    kind === 'chat' ? chatEventLabel(filters.value.eventType) : filters.value.eventType;
  const failures = recentFailures(trigger.id, deliveries);
  const succeeded = mine.reduce(
    (n, d) =>
      n + d.runs.filter((r) => r.triggerId === trigger.id && isStartedOutcome(r.status)).length,
    0,
  );
  const shown = mine.slice(0, DELIVERY_SAMPLE);
  const endpoint = endpointFor(ingestHost, trigger.sourceId);
  // A webhook or chat trigger on a live source has a secret to rotate.
  const hasSigningSecret = trigger.kind === 'webhook' && !trigger.sourceMissing;

  const webhookFields = schemaCatalog(sourceCatalog.fields);
  const promptCatalog =
    trigger.kind === 'webhook'
      ? webhookFields
      : trigger.kind === 'scheduled'
        ? toTemplateCatalog(scheduledCatalog())
        : toTemplateCatalog(manualInputsCatalog(trigger.manualInputs));

  const setFilterRows = (rows: FilterRow[]) =>
    filters.setValue({ ...filters.value, filters: rows });

  const firesOn = trigger.eventType
    ? kind === 'chat'
      ? chatEventLabel(trigger.eventType)
      : trigger.eventType
    : 'no event type (never fires)';
  const subtitle =
    trigger.kind === 'webhook'
      ? `${kind === 'chat' ? 'Chat command' : 'Webhook'} · ${source?.name ?? (trigger.sourceMissing ? 'no source' : trigger.sourceId)} · ${firesOn}`
      : trigger.kind === 'scheduled'
        ? `Schedule · ${describeCron(trigger.scheduleConfig?.cronExpression ?? '')}`
        : 'Manual · run on demand';

  const managedMessage = managedFooterMessage(trigger.declared, trigger.repository);
  // One notice covers every disabled edit control in the modal.
  const notice = managed ? (
    <Stack gap="snug">
      {trigger.declared && (
        <Stack direction="horizontal">
          <ManagedInGitBadge repository={trigger.repository} />
        </Stack>
      )}
      <ManagedInGitNotice declared={trigger.declared} repository={trigger.repository}>
        {trigger.autoPausedAt !== null &&
          ' You can still re-enable this trigger after its auto-pause.'}
      </ManagedInGitNotice>
    </Stack>
  ) : !canWrite ? (
    <Alert variant="info">{permissionHintText('edit triggers')}</Alert>
  ) : (
    !canRunAgent && (
      <Alert variant="info">
        {permissionHintText("run this trigger's agent")} Its prompt, filters, schedule and on or off
        state are read only.
      </Alert>
    )
  );

  // The sections follow the wizard's steps, in its order: what the trigger is
  // and listens for, what it asks, which agent it starts, how runs are admitted.
  const configSections: (ConfigurationSection & { description: string })[] = [
    {
      id: 'trigger',
      icon: 'zap' as const,
      dirty: name.dirty,
      title: 'Trigger',
      sub: trigger.kind === 'webhook' ? 'Name and source' : 'Name',
      description:
        'What this trigger is called' +
        (trigger.kind === 'webhook' ? ' and where it listens.' : '.'),
    },
    ...(trigger.kind === 'scheduled'
      ? [
          {
            id: 'schedule',
            icon: 'clock' as const,
            dirty: schedule.dirty,
            title: 'Schedule',
            sub: 'When it fires',
            description: 'When this trigger starts a run on its own.',
          },
        ]
      : []),
    ...(trigger.kind === 'webhook'
      ? [
          {
            id: 'filters',
            icon: 'filter' as const,
            dirty: filters.dirty,
            title: 'Event filters',
            sub: 'What starts a run',
            description:
              'The event this trigger listens for, and which of its deliveries start a run.',
          },
          // A provider that cannot say who caused an event has no senders to restrict.
          ...(sourceCatalog.identifiesSenders
            ? [
                {
                  id: 'senders',
                  icon: 'user' as const,
                  dirty: senders.dirty,
                  title: 'Senders',
                  sub: 'Who may fire it',
                  description: 'Limit which people and bots can start a run.',
                },
              ]
            : []),
        ]
      : []),
    {
      id: 'prompt',
      icon: 'scroll-text' as const,
      dirty: prompt.dirty,
      title: 'Prompt',
      sub: 'What the agent is asked',
      description: 'What each run is asked to do. Fields from the delivery fill the template.',
    },
    {
      id: 'delivery',
      icon: 'send' as const,
      dirty: delivery.dirty,
      title: 'Delivery',
      sub: 'How runs are admitted',
      description:
        "Control how many runs this trigger starts at once, and what happens to events that arrive while it's at the limit.",
    },
    // Last, and in the danger colours: the actions that cannot be taken back.
    ...(hasSigningSecret || !managed
      ? [
          {
            id: 'danger',
            icon: 'triangle-alert' as const,
            title: 'Danger zone',
            sub: 'Rotate or delete',
            description: 'Actions that cannot be undone.',
            tone: 'danger' as const,
          },
        ]
      : []),
  ];
  const activeSection = configSections.find((x) => x.id === section) ?? configSections[0]!;

  return (
    <Modal open onClose={handleClose} size="builder">
      <ModalHeader
        title={trigger.name}
        description={subtitle}
        titleAccessory={
          <Toggle
            checked={trigger.enabled}
            onChange={(next) => onToggleEnabled(trigger, next)}
            disabled={!canShapeRuns || !triggerToggleOpen(trigger, tenantLocked)}
            aria-label={`${trigger.enabled ? 'Disable' : 'Enable'} ${trigger.name}`}
          />
        }
        actions={
          configSections.some((x) => x.dirty) ? (
            <Stack direction="horizontal" align="center" gap="snug">
              <StatusDot status="degraded" size="sm" />
              <Text as="span" size="caption" color="subtle">
                Unsaved changes
              </Text>
            </Stack>
          ) : undefined
        }
        borderless={false}
      />

      {/* Tabs own the modal's vertical space so the strip sits flush under the
          header with its own full-bleed border, and only the body scrolls. */}
      <Tabs value={tab} onValueChange={setTab} size="compact" fill>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="configuration">Configuration</TabsTrigger>
        </TabsList>

        {tab === 'configuration' ? (
          <TabsContent value="configuration">
            <Stack
              direction={{ base: 'vertical', lg: 'horizontal' }}
              height="full"
              overflow="hidden"
              gap="none"
            >
              <ConfigurationRail
                label="Configuration"
                sections={configSections}
                current={activeSection.id}
                onSelect={setSection}
              />
              <Wizard.Step>
                <Stack gap="snug">
                  <Heading variant="subsection">{activeSection.title}</Heading>
                  <Text size="supporting" color="muted">
                    {activeSection.description}
                  </Text>
                </Stack>
                {managed ? (
                  <ManagedSectionNotice
                    declared={trigger.declared}
                    repository={trigger.repository}
                    extra={
                      activeSection.id === 'danger'
                        ? `Deleting this trigger is also done there.${
                            onRotateSecret !== undefined && hasSigningSecret
                              ? ' Rotating the signing secret still works here.'
                              : ''
                          }`
                        : undefined
                    }
                  />
                ) : (
                  notice
                )}
                {activeSection.id === 'trigger' && (
                  <Stack gap="relaxed">
                    <Stack gap="snug">
                      <TriggerNameField
                        value={name.value}
                        onChange={name.setValue}
                        hint="Shown in the triggers list and on every run it starts."
                        placeholder=""
                        disabled={!editable}
                        invalid={name.value.trim() === ''}
                      />
                    </Stack>
                    {trigger.kind === 'webhook' && (
                      <SectionField
                        label="Source"
                        mode="edit"
                        mutability="create-only"
                        help="Where this trigger's deliveries come from."
                      >
                        <Input
                          mono
                          disabled
                          aria-label="Source"
                          value={
                            source?.name ?? (trigger.sourceMissing ? 'No source' : trigger.sourceId)
                          }
                        />
                      </SectionField>
                    )}
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
                )}
                {activeSection.id === 'schedule' && (
                  <>
                    {/* Schedule: the same builder the New trigger wizard uses. */}
                    {trigger.kind === 'scheduled' && trigger.scheduleConfig && (
                      <Stack gap="relaxed">
                        <ScheduleBuilder
                          value={scheduleDraft}
                          onChange={patchSchedule}
                          zones={zones}
                          disabled={!runEditable}
                        />
                        <EditActions
                          dirty={schedule.dirty}
                          pending={schedule.pending}
                          error={schedule.error}
                          blocker={schedule.blocker}
                          saveLabel="Save schedule"
                          onSave={schedule.save}
                          onDiscard={schedule.discard}
                        />
                      </Stack>
                    )}
                  </>
                )}
                {activeSection.id === 'filters' && trigger.kind === 'webhook' && (
                  <Stack gap="snug">
                    <EventFiltersSection
                      category={kind === 'chat' ? 'chat' : 'webhook'}
                      catalog={sourceCatalog}
                      eventType={filters.value.eventType}
                      filters={filters.value.filters}
                      onEventTypeChange={(eventType) =>
                        filters.setValue({ ...filters.value, eventType })
                      }
                      onFiltersChange={setFilterRows}
                      disabled={!runEditable}
                      schemaless={schemaless}
                    />
                    <EditActions
                      dirty={filters.dirty}
                      pending={filters.pending}
                      error={filters.error}
                      blocker={filtersBlocker}
                      saveLabel="Save filters"
                      onSave={filters.save}
                      onDiscard={filters.discard}
                    />
                  </Stack>
                )}
                {activeSection.id === 'senders' && (
                  <>
                    {/* Senders: who may fire it, when the provider says who caused an event. */}
                    {trigger.kind === 'webhook' && (
                      <SenderRestrictionEditor
                        identifiesSenders={sourceCatalog.identifiesSenders}
                        roles={sourceCatalog.roles}
                        idPlaceholder={SENDER_ID_EXAMPLE[kind === 'chat' ? 'chat' : 'webhook']}
                        value={senders.value}
                        onChange={senders.setValue}
                        disabled={!runEditable}
                        hideHeading
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
                  </>
                )}
                {activeSection.id === 'prompt' && (
                  <>
                    {/* Prompt: what the run is actually asked to do. */}
                    <Stack gap="snug">
                      <TemplateField
                        code
                        aria-label="Prompt template"
                        value={prompt.value}
                        onChange={prompt.setValue}
                        catalog={promptCatalog}
                        disabled={!runEditable}
                        placeholder="What this trigger asks the agent to do."
                        previewExtra={
                          trigger.kind === 'webhook' ? (
                            <DataBlockPreview template={prompt.value} />
                          ) : undefined
                        }
                        catalogLabel={
                          trigger.kind === 'webhook' && filters.value.eventType
                            ? `Fields for ${eventName} ${kind === 'chat' ? 'messages' : 'deliveries'}`
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
                  </>
                )}
                {activeSection.id === 'delivery' && (
                  <DeliverySection
                    value={delivery.value}
                    kind={trigger.kind}
                    coalesceKeyExample={
                      kind === 'chat' ? chatConversationKey(filters.value.eventType) : undefined
                    }
                    disabled={!editable}
                    onChange={(next) => delivery.setValue({ ...delivery.value, ...next })}
                    actions={
                      <EditActions
                        dirty={delivery.dirty}
                        pending={delivery.pending}
                        error={delivery.error}
                        blocker={delivery.blocker}
                        alwaysShow
                        readOnly={!editable}
                        notice={
                          managed
                            ? managedMessage
                            : canWrite
                              ? undefined
                              : "You don't have permission to change delivery settings."
                        }
                        saveLabel="Save delivery"
                        onSave={delivery.save}
                        onDiscard={delivery.discard}
                      />
                    }
                  />
                )}
                {activeSection.id === 'danger' && (
                  <IssuedSecret
                    key={trigger.id}
                    waiting={source !== undefined && !source.signingSecretSet}
                    placeholder={
                      genericMethod !== undefined
                        ? 'Or paste a secret your sender issued. It replaces the stored one and is never shown back.'
                        : undefined
                    }
                    onReplace={
                      onRotateSecret !== undefined && source !== undefined
                        ? (secret) =>
                            onRotateSecret(trigger, secret, !source.signingSecretSet).then((r) => {
                              if (r === null) return false;
                              // A generated secret shown earlier stopped working when this one landed.
                              setRotatedSecret(null);
                              return true;
                            })
                        : undefined
                    }
                  >
                    {({ field, actions }) => {
                      // Slack's secret is only ever pasted; a generic HMAC or Standard
                      // Webhooks sender may issue its own, or be given a generated one.
                      const slack = source?.provider === SLACK_PROVIDER;
                      const pastes = slack || takesPastedSecret(genericMethod ?? '');
                      return (
                        <Stack gap="comfortable">
                          {hasSigningSecret && source !== undefined && (
                            <DangerZone
                              tone="warning"
                              title={
                                <Stack direction="horizontal" gap="tight" align="center">
                                  <Text as="span" size="supporting" weight="emphasis">
                                    Signing secret
                                  </Text>
                                  <SigningInfo
                                    note={
                                      genericMethod === undefined
                                        ? undefined
                                        : verificationNote(genericMethod)
                                    }
                                  />
                                  {slack && !source.signingSecretSet && (
                                    <Badge variant="degraded" size="sm" shape="pill">
                                      Waiting for signing secret
                                    </Badge>
                                  )}
                                </Stack>
                              }
                              description={
                                slack
                                  ? !source.signingSecretSet
                                    ? `Waiting for the signing secret. Copy it from your ${providerName(source.provider)} app's Basic Information page and paste it here.`
                                    : `Regenerate it in your ${providerName(source.provider)} app, then paste the new secret here.`
                                  : genericMethod !== undefined
                                    ? `${verificationNote(genericMethod)} Rotating replaces the secret deliveries are signed with; the old one stops working the moment you rotate.`
                                    : 'Replaces the secret deliveries are signed with. The old one stops working the moment you rotate.'
                              }
                              action={
                                slack ? undefined : (
                                  <Button
                                    variant="warning"
                                    size="sm"
                                    disabled={onRotateSecret === undefined || rotating}
                                    onClick={() => setRotateOpen(true)}
                                  >
                                    {rotating ? 'Rotating…' : 'Rotate'}
                                  </Button>
                                )
                              }
                            >
                              {/* A secret the provider issues is pasted in, never generated here. */}
                              {pastes && field}
                              {/* The stored secret is write-once and never returned on
                              reads; only a rotation mints something to show. */}
                              {!slack && rotatedSecret !== null && (
                                <Stack gap="snug">
                                  <PlainRow
                                    actions={<CopyButton value={rotatedSecret} label="Copy" />}
                                  >
                                    <Text mono size="caption">
                                      {rotatedSecret}
                                    </Text>
                                  </PlainRow>
                                  <Text size="caption" color="subtle">
                                    Update your provider with this secret now. It is shown once and
                                    cannot be recovered.
                                  </Text>
                                </Stack>
                              )}
                              {/* Requests that fail authentication never become deliveries;
                              this counter is the only place a wrong secret shows up. */}
                              {source.rejectionCount > 0 && (
                                <Text size="caption" color="status-error">
                                  {source.rejectionCount} rejected
                                  {source.lastRejectionReason !== null
                                    ? `: ${source.lastRejectionReason}`
                                    : ''}
                                </Text>
                              )}
                            </DangerZone>
                          )}
                          {!managed && (
                            <DangerZone
                              title={
                                <Text as="span" size="supporting" weight="emphasis">
                                  Delete trigger
                                </Text>
                              }
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
                          )}
                          {/* Last, like every save row in the modal: Slack's secret is the one
                          field here with something to save. */}
                          {hasSigningSecret && pastes && actions}
                        </Stack>
                      );
                    }}
                  </IssuedSecret>
                )}
              </Wizard.Step>
            </Stack>
          </TabsContent>
        ) : (
          <Stack grow overflow="hidden" gap="none">
            {/* ── Overview: what this is and how it is doing ─────────────── */}
            <TabsContent value="overview">
              <Stack gap="comfortable" padding="relaxed">
                {notice}
                {/* One row once the modal is wide enough; two columns below that. */}
                <Grid
                  cols={trigger.pendingCount > 0 ? { base: 2, sm: 3, md: 5 } : { base: 2, md: 4 }}
                  gap="snug"
                >
                  <DetailStat label="Deliveries 24h" value={String(mine.length)} />
                  <DetailStat
                    label="Succeeded"
                    value={String(succeeded)}
                    tone={succeeded > 0 ? 'healthy' : undefined}
                  />
                  <DetailStat
                    label="Failed"
                    value={String(failures)}
                    tone={failures > 0 ? 'error' : undefined}
                  />
                  <DetailStat
                    label="Consecutive failures"
                    value={String(trigger.consecutiveFailures)}
                    tone={trigger.consecutiveFailures > 0 ? 'error' : undefined}
                  />
                  {/* Only when something is actually waiting: a permanent
                    "0 waiting" would read as a queue that exists rather than
                    as a trigger keeping up. */}
                  {trigger.pendingCount > 0 && (
                    <DetailStat label="Waiting now" value={String(trigger.pendingCount)} />
                  )}
                </Grid>

                {trigger.autoPausedAt !== null && (
                  <Alert variant="error">
                    The platform disabled this trigger after {trigger.consecutiveFailures}{' '}
                    consecutive failures. Fix the cause, then re-enable it above.
                  </Alert>
                )}

                {trigger.sourceMissing && (
                  <Alert variant="error">
                    The source of this trigger was deleted. It no longer fires and can only be
                    turned off or deleted.
                  </Alert>
                )}

                {/* Webhook: the address to paste into a provider, and the secret
                  that proves deliveries came from it. */}
                {trigger.kind === 'webhook' && !trigger.sourceMissing && (
                  <>
                    <Stack gap="snug">
                      <Stack direction="horizontal" gap="snug" align="center">
                        <SectionLabel>{kind === 'chat' ? 'Request URL' : 'Endpoint'}</SectionLabel>
                        {source?.declared && <ManagedInGitBadge repository={source.repository} />}
                      </Stack>
                      <Card subtle border="subtle">
                        <Stack paddingInline="cozy">
                          <CopyField value={endpoint} mono bordered={false} />
                        </Stack>
                      </Card>
                    </Stack>
                  </>
                )}

                {/* Manual: the form this trigger publishes, and the way to run it. */}
                {trigger.kind === 'manual' && (
                  <Stack gap="snug">
                    <SectionLabel>Run this agent</SectionLabel>
                    <Card subtle border="subtle" divide column>
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
                    <Link
                      href={APP_ROUTES.agents.detail(trigger.agentSlug)}
                      aria-label={`Open agent ${trigger.agentSlug}`}
                      className="block rounded-lg transition-opacity hover:opacity-80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                    >
                      <PlainRow
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
                      </PlainRow>
                    </Link>
                  )}
                </Stack>

                {/* Deliveries: what it has actually done. Honest about sampling:
                  the count says how many of the most recent are shown. */}
                <DeliveryList
                  deliveries={shown}
                  triggerId={trigger.id}
                  expandedId={expanded}
                  onToggleExpanded={setExpanded}
                  onRedeliver={redeliverAction}
                  redeliverAllowed={canShapeRuns}
                  agents={agents}
                  runHref={
                    trigger.routing === null && trigger.agentSlug !== ''
                      ? (runId) => APP_ROUTES.agents.run(trigger.agentSlug, runId)
                      : undefined
                  }
                  sourceName={source?.name}
                  eventType={eventName}
                />
              </Stack>
            </TabsContent>
          </Stack>
        )}
      </Tabs>
      <ConfirmDialog
        open={confirmingDiscard}
        title="Discard unsaved changes?"
        description="You have edits that have not been saved. Closing this trigger discards them."
        confirmLabel="Discard changes"
        onConfirm={discardAndClose}
        onClose={() => setConfirmingDiscard(false)}
      />
      <ConfirmDialog
        open={rotateOpen}
        title="Rotate signing secret"
        description={`Rotate the signing secret for "${source?.name ?? trigger.name}"? The secret configured in your provider stops working the moment this completes, and deliveries signed with it are rejected until you update the provider.`}
        confirmLabel="Rotate secret"
        confirming={rotating}
        error={rotateError}
        onConfirm={() => {
          setRotating(true);
          setRotateError(null);
          void onRotateSecret?.(trigger).then((secret) => {
            setRotating(false);
            if (secret === null) {
              setRotateError('Could not rotate the secret.');
              return;
            }
            setRotateOpen(false);
            setRotatedSecret(secret);
          });
        }}
        onClose={() => {
          setRotateOpen(false);
          setRotateError(null);
        }}
      />
    </Modal>
  );
}

/**
 * TriggerDetailModal shows one trigger's configuration, history and settings.
 *
 * Split three ways because those are three different questions: what is this,
 * what has it done, and how should it behave. The Overview leads with the thing
 * an operator most often came for — the endpoint to paste into a provider, the
 * schedule to sanity-check, the form to run.
 */
export function TriggerDetailModal(props: TriggerDetailModalProps) {
  // The body mounts only while a trigger is open, so everything it holds (the tab,
  // the open section, drafts, the expanded delivery, a revealed secret) is lost on
  // close, and a different trigger starts from a clean slate.
  if (props.trigger === null) return null;
  return <TriggerDetailModalBody key={props.trigger.id} {...props} />;
}
