'use client';

/** @tier 2 — Orbit-domain-aware: webhook trigger authoring (event → agent + prompt).
 * Used by: triggers platform section, agents per-agent Triggers tab. */

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
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
import type { TriggerCreateRequest, TriggerUpdateRequest } from '@orbit/shared/api';

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
  name: string;
  sourceId: string;
  agentTypeId: string;
  agentSlug: string;
  eventType: string;
  matchCondition: Record<string, string[]>;
  promptTemplate: string;
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
    if (!isEdit && !sourceId) {
      setSubmitError('Select a webhook source.');
      return;
    }
    if (!isEdit && !resolvedAgent) {
      setSubmitError('Select a target agent.');
      return;
    }
    if (!eventType.trim()) {
      setSubmitError('Event type is required.');
      return;
    }
    if (!promptTemplate.trim()) {
      setSubmitError('Prompt template is required.');
      return;
    }

    setSubmitting(true);
    setSubmitError(null);
    const matchCondition = rowsToCondition(conditions);

    let res: Response;
    try {
      if (isEdit) {
        const payload: TriggerUpdateRequest = {
          name: trimmedName,
          event_type: eventType.trim(),
          match_condition: matchCondition,
          prompt_template: promptTemplate,
        };
        res = await fetch(ApiRoute.triggers.detail(trigger.id), {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
      } else if (resolvedAgent) {
        const payload: TriggerCreateRequest = {
          source_id: sourceId,
          name: trimmedName,
          agent_type_id: resolvedAgent.id,
          agent_slug: resolvedAgent.slug,
          event_type: eventType.trim(),
          match_condition: matchCondition,
          prompt_template: promptTemplate,
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
    sourceId,
    resolvedAgent,
    eventType,
    promptTemplate,
    conditions,
    trigger,
    onSaved,
  ]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader
        title={isEdit ? 'Edit trigger' : 'New trigger'}
        description="Run an agent automatically when a matching event arrives on a webhook source."
      />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <FormField id="trigger-name" label="Name">
            <Input
              id="trigger-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Review opened PRs"
              maxLength={128}
            />
          </FormField>

          {!isEdit && (
            <FormField id="trigger-source" label="Webhook source">
              <NativeSelect
                id="trigger-source"
                value={sourceId}
                onChange={(e) => setSourceId(e.target.value)}
              >
                {sources.length === 0 && <option value="">No sources — create one first</option>}
                {sources.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </NativeSelect>
            </FormField>
          )}

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
              Optional. Each row requires a payload field to equal one of the comma-separated values
              (e.g. action = opened, synchronize · pr.base_ref = main).
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

          <FormField
            id="trigger-prompt"
            label="Prompt template"
            hint="Sent as the run's initial message. Use {{ field }} placeholders, e.g. {{ pr.title }}."
          >
            <Textarea
              id="trigger-prompt"
              value={promptTemplate}
              onChange={(e) => setPromptTemplate(e.target.value)}
              placeholder={
                'Review PR #{{ pr.number }}: {{ pr.title }} on branch {{ pr.head_ref }}.'
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
