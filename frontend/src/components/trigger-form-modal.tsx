'use client';

/** @tier 2 — Orbit-domain-aware: trigger authoring (webhook / scheduled / manual → agent + prompt).
 * Used by: triggers platform section, agents per-agent Triggers tab. */

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Code,
  FormField,
  Icon,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { TriggerCreateRequest, TriggerKind, TriggerUpdateRequest } from '@orbit/shared/api';

export interface TriggerFormSource {
  id: string;
  name: string;
}

export interface TriggerFormAgent {
  id: string;
  slug: string;
  name: string;
}

/** The editable shape of a trigger, decoupled from any feature's row type. */
export interface TriggerFormValue {
  id: string;
  kind: TriggerKind;
  name: string;
  sourceId: string;
  agentTypeId: string;
  agentSlug: string;
  eventType: string;
  matchCondition: Record<string, string[]>;
  promptTemplate: string;
  scheduleConfig?: { cronExpression: string; timezone: string };
}

export interface TriggerFormModalProps {
  open: boolean;
  /** Existing trigger to edit, or null to create a new one. */
  trigger: TriggerFormValue | null;
  /** Webhook sources available to bind to. */
  sources: TriggerFormSource[];
  /** Agent types selectable as the run target. Ignored when `lockedAgent` is set. */
  agents: TriggerFormAgent[];
  /** When set, the agent is fixed (per-agent tab) and the picker is hidden. */
  lockedAgent?: TriggerFormAgent;
  /** Pre-selected source for new triggers. */
  defaultSourceId?: string;
  onClose: () => void;
  onSaved: () => void;
}

interface ConditionRow {
  field: string;
  values: string;
}

function conditionToRows(condition: Record<string, string[]>): ConditionRow[] {
  return Object.entries(condition).map(([field, values]) => ({ field, values: values.join(', ') }));
}

function rowsToCondition(rows: ConditionRow[]): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const row of rows) {
    const field = row.field.trim();
    if (!field) continue;
    const values = row.values
      .split(',')
      .map((v) => v.trim())
      .filter(Boolean);
    if (values.length > 0) out[field] = values;
  }
  return out;
}

const KIND_LABELS: Record<TriggerKind, string> = {
  webhook: 'Webhook — fires when an inbound HTTP event matches',
  scheduled: 'Scheduled — fires on a cron schedule',
  manual: 'Manual — fired by an agent or API call',
};

export function TriggerFormModal({
  open,
  trigger,
  sources,
  agents,
  lockedAgent,
  defaultSourceId,
  onClose,
  onSaved,
}: TriggerFormModalProps) {
  const isEdit = trigger !== null;

  const [kind, setKind] = useState<TriggerKind>(trigger?.kind ?? 'webhook');
  const [name, setName] = useState(trigger?.name ?? '');
  const [sourceId, setSourceId] = useState(
    trigger?.sourceId ?? defaultSourceId ?? sources[0]?.id ?? '',
  );
  const [agentId, setAgentId] = useState(
    trigger?.agentTypeId ?? lockedAgent?.id ?? agents[0]?.id ?? '',
  );
  const [eventType, setEventType] = useState(trigger?.eventType ?? 'pull_request');
  const [promptTemplate, setPromptTemplate] = useState(trigger?.promptTemplate ?? '');
  const [conditions, setConditions] = useState<ConditionRow[]>(
    trigger ? conditionToRows(trigger.matchCondition) : [],
  );
  const [cronExpression, setCronExpression] = useState(
    trigger?.scheduleConfig?.cronExpression ?? '0 9 * * 1-5',
  );
  const [timezone, setTimezone] = useState(trigger?.scheduleConfig?.timezone ?? 'UTC');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const resolvedAgent = useMemo(
    () => lockedAgent ?? agents.find((a) => a.id === agentId) ?? null,
    [lockedAgent, agents, agentId],
  );

  const updateCondition = useCallback((idx: number, patch: Partial<ConditionRow>) => {
    setConditions((prev) => prev.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
  }, []);

  const handleSubmit = useCallback(async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setSubmitError('Name is required.');
      return;
    }
    if (!isEdit && !resolvedAgent) {
      setSubmitError('Select a target agent.');
      return;
    }
    if (!isEdit && kind === 'webhook' && !sourceId) {
      setSubmitError('Select a webhook source.');
      return;
    }
    if (kind === 'webhook' && !eventType.trim()) {
      setSubmitError('Event type is required.');
      return;
    }
    if (kind === 'scheduled' && !cronExpression.trim()) {
      setSubmitError('Cron expression is required.');
      return;
    }
    if (!promptTemplate.trim()) {
      setSubmitError('Prompt template is required.');
      return;
    }

    setSubmitting(true);
    setSubmitError(null);

    let res: Response;
    try {
      if (isEdit) {
        const payload: TriggerUpdateRequest = {
          name: trimmedName,
          prompt_template: promptTemplate,
          ...(kind === 'webhook'
            ? {
                event_type: eventType.trim(),
                match_condition: rowsToCondition(conditions),
              }
            : {}),
          ...(kind === 'scheduled'
            ? {
                schedule_config: {
                  cron_expression: cronExpression.trim(),
                  timezone: timezone.trim() || 'UTC',
                },
              }
            : {}),
        };
        res = await fetch(ApiRoute.triggers.detail(trigger.id), {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
      } else if (resolvedAgent) {
        const payload: TriggerCreateRequest = {
          kind,
          name: trimmedName,
          agent_type_id: resolvedAgent.id,
          agent_slug: resolvedAgent.slug,
          prompt_template: promptTemplate,
          ...(kind === 'webhook'
            ? {
                source_id: sourceId,
                event_type: eventType.trim(),
                match_condition: rowsToCondition(conditions),
              }
            : {}),
          ...(kind === 'scheduled'
            ? {
                schedule_config: {
                  cron_expression: cronExpression.trim(),
                  timezone: timezone.trim() || 'UTC',
                },
              }
            : {}),
        };
        res = await fetch(ApiRoute.triggers.create(), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
      } else {
        setSubmitting(false);
        setSubmitError('Select a target agent.');
        return;
      }
    } catch {
      setSubmitting(false);
      setSubmitError('Network error — could not reach the server.');
      return;
    }

    setSubmitting(false);
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as { message?: string };
      setSubmitError(body.message ?? `Request failed (${res.status})`);
      return;
    }
    onSaved();
  }, [
    name,
    isEdit,
    kind,
    sourceId,
    resolvedAgent,
    eventType,
    promptTemplate,
    conditions,
    cronExpression,
    timezone,
    trigger,
    onSaved,
  ]);

  const modalDescription =
    kind === 'scheduled'
      ? 'Run an agent on a repeating cron schedule.'
      : kind === 'manual'
        ? 'Run an agent on demand via an API call or from another agent.'
        : 'Run an agent automatically when a matching event arrives on a webhook source.';

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader title={isEdit ? 'Edit trigger' : 'New trigger'} description={modalDescription} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          {!isEdit && (
            <FormField id="trigger-kind" label="Trigger type">
              <NativeSelect
                id="trigger-kind"
                value={kind}
                onChange={(e) => setKind(e.target.value as TriggerKind)}
              >
                {(Object.keys(KIND_LABELS) as TriggerKind[]).map((k) => (
                  <option key={k} value={k}>
                    {KIND_LABELS[k]}
                  </option>
                ))}
              </NativeSelect>
            </FormField>
          )}

          <FormField id="trigger-name" label="Name">
            <Input
              id="trigger-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Review opened PRs"
              maxLength={128}
            />
          </FormField>

          {!isEdit && !lockedAgent && (
            <FormField id="trigger-agent" label="Target agent">
              <NativeSelect
                id="trigger-agent"
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
              >
                {agents.length === 0 && <option value="">No agents available</option>}
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </NativeSelect>
            </FormField>
          )}

          {lockedAgent && !isEdit && (
            <Text size="caption" color="muted">
              Target agent: {lockedAgent.name}
            </Text>
          )}

          {/* ── Webhook-specific fields ────────────────────────────────── */}
          {kind === 'webhook' && (
            <>
              {!isEdit && (
                <FormField id="trigger-source" label="Webhook source">
                  <NativeSelect
                    id="trigger-source"
                    value={sourceId}
                    onChange={(e) => setSourceId(e.target.value)}
                  >
                    {sources.length === 0 && (
                      <option value="">No sources — create one first</option>
                    )}
                    {sources.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </NativeSelect>
                </FormField>
              )}

              <FormField
                id="trigger-event"
                label="Event type"
                hint="e.g. pull_request or push (the provider's event name)."
              >
                <Input
                  id="trigger-event"
                  value={eventType}
                  onChange={(e) => setEventType(e.target.value)}
                  placeholder="pull_request"
                />
              </FormField>

              <Stack gap="snug">
                <Text size="caption" weight="emphasis">
                  Match conditions
                </Text>
                <Text size="caption" color="muted">
                  Optional. Each row requires a payload field to equal one of the comma-separated
                  values (e.g. action = opened, synchronize · pr.base_ref = main).
                </Text>
                {conditions.map((row, idx) => (
                  <Stack key={idx} direction="horizontal" gap="snug" align="center">
                    <Input
                      value={row.field}
                      onChange={(e) => updateCondition(idx, { field: e.target.value })}
                      placeholder="field (e.g. action)"
                    />
                    <Input
                      value={row.values}
                      onChange={(e) => updateCondition(idx, { values: e.target.value })}
                      placeholder="opened, synchronize"
                    />
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setConditions((prev) => prev.filter((_, i) => i !== idx))}
                      aria-label="Remove condition"
                    >
                      <Icon name="trash" size="2xs" />
                    </Button>
                  </Stack>
                ))}
                <Stack direction="horizontal">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setConditions((prev) => [...prev, { field: '', values: '' }])}
                  >
                    <Icon name="plus" size="xs" />
                    Add condition
                  </Button>
                </Stack>
              </Stack>
            </>
          )}

          {/* ── Scheduled-specific fields ──────────────────────────────── */}
          {kind === 'scheduled' && (
            <>
              <FormField
                id="trigger-cron"
                label="Cron expression"
                hint="Standard 5-field cron (minute hour dom month dow). e.g. 0 9 * * 1-5 = weekdays at 9 AM."
              >
                <Input
                  id="trigger-cron"
                  value={cronExpression}
                  onChange={(e) => setCronExpression(e.target.value)}
                  placeholder="0 9 * * 1-5"
                  spellCheck={false}
                />
              </FormField>
              <FormField
                id="trigger-tz"
                label="Timezone"
                hint="IANA timezone name (e.g. America/New_York). Defaults to UTC."
              >
                <Input
                  id="trigger-tz"
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                  placeholder="UTC"
                />
              </FormField>
            </>
          )}

          {/* ── Manual — informational note ───────────────────────────── */}
          {kind === 'manual' && !isEdit && (
            <Text size="caption" color="muted">
              After creation you will receive a trigger ID. Any principal with the{' '}
              <Code>triggers:fire:&#123;id&#125;</Code> scope can POST to{' '}
              <Code>/api/v1/triggers/&#123;id&#125;/fire</Code> to dispatch a run.
            </Text>
          )}

          <FormField
            id="trigger-prompt"
            label="Prompt template"
            hint={
              kind === 'scheduled'
                ? 'Use {{ var.date }}, {{ var.datetime }}, {{ var.trigger_id }} for injected values.'
                : kind === 'manual'
                  ? 'Use {{ var.KEY }} for caller-supplied variables.'
                  : "Sent as the run's initial message. Use {{ field }} placeholders, e.g. {{ pr.title }}."
            }
          >
            <Textarea
              id="trigger-prompt"
              value={promptTemplate}
              onChange={(e) => setPromptTemplate(e.target.value)}
              placeholder={
                kind === 'scheduled'
                  ? 'Run the daily standup digest for {{ var.date }}.'
                  : kind === 'manual'
                    ? 'Process the following input: {{ var.input }}'
                    : 'Review PR #{{ pr.number }}: {{ pr.title }} on branch {{ pr.head_ref }}.'
              }
              rows={4}
            />
          </FormField>

          {submitError && <Alert variant="error">{submitError}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => void handleSubmit()} disabled={submitting || !name.trim()}>
          {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create trigger'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
