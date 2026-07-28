'use client';

/** @tier 3 — triggers-scoped. The webhook kind: an external system posts a
 * signed event to an Orbit endpoint. Step, review rows, draft, done block. */

import {
  FieldRow,
  FormField,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { CopyButton } from '@orbit/components';
import { FIELD_IDS, VALID, firstBlocker, validateName, validatePrompt } from '../wizard-state';
import { NEW_SOURCE } from '../types';
import type {
  DoneContentProps,
  StepComponentProps,
  StepValidation,
  TriggerKindDefinition,
  WizardState,
} from '../types';

/** Providers with a signature adapter in the backend. Nothing else verifies. */
const PROVIDERS: { id: string; name: string; description: string; icon: IconName }[] = [
  {
    id: 'github',
    name: 'GitHub',
    description: 'Repository and organisation webhooks, verified by HMAC signature.',
    icon: 'code-2',
  },
  {
    id: 'alertmanager',
    name: 'Prometheus Alertmanager',
    description: 'Alert notifications, verified by bearer token.',
    icon: 'bell',
  },
];

/** Common event types per provider. Any other value the API accepts still works. */
const EVENTS: Record<string, string[]> = {
  github: ['push', 'pull_request', 'issues', 'release', 'workflow_run'],
  alertmanager: ['firing', 'resolved'],
};

/** The inbound path is owned by the source, so the URL is derivable from its id. */
function endpointFor(ingestHost: string, sourceId: string): string {
  return `https://${ingestHost}/api/v1/webhooks/${sourceId}`;
}

function StepSource({ state, patch, context, fieldError, firstFieldRef }: StepComponentProps) {
  const { sources, ingestHost } = context;
  const selected = sources.find((s) => s.id === state.sourceChoice);
  const isNew = state.sourceChoice === NEW_SOURCE;
  const provider = isNew ? state.newSourceProvider : (selected?.provider ?? '');
  const events = EVENTS[provider] ?? [];

  return (
    <Stack gap="relaxed">
      <FormField
        label="Trigger name"
        id={FIELD_IDS.name}
        hint="Shown in the triggers list and on every run it starts."
        error={fieldError(FIELD_IDS.name)}
        required
      >
        <Input
          id={FIELD_IDS.name}
          ref={firstFieldRef}
          mono
          value={state.name}
          onChange={(e) => patch({ name: e.target.value })}
          placeholder="github-push"
          maxLength={128}
        />
      </FormField>

      <Stack gap="cozy" id={FIELD_IDS.source}>
        <SectionLabel>Source</SectionLabel>
        <RadioGroup
          value={state.sourceChoice}
          onValueChange={(v) => patch({ sourceChoice: v })}
          aria-label="Source"
        >
          <Grid cols={2} gap="cozy">
            {sources.map((s) => (
              <RadioCard
                key={s.id}
                value={s.id}
                selected={state.sourceChoice === s.id}
                onSelect={(v) => patch({ sourceChoice: v })}
                icon="webhook"
                title={s.name}
                description={s.provider}
              />
            ))}
            <RadioCard
              value={NEW_SOURCE}
              selected={isNew}
              onSelect={(v) => patch({ sourceChoice: v })}
              icon="plus"
              title="New source"
              description="Register a new origin and get its signing secret."
            />
          </Grid>
        </RadioGroup>

        <FieldRow
          leading={
            <Text as="span" size="caption" color="subtle">
              Endpoint
            </Text>
          }
        >
          <Text as="span" size="caption" mono color="muted" truncate>
            {isNew || !state.sourceChoice
              ? `https://${ingestHost}/api/v1/webhooks/<issued on create>`
              : endpointFor(ingestHost, state.sourceChoice)}
          </Text>
        </FieldRow>

        {isNew && (
          <>
            <FormField
              label="Source name"
              id={FIELD_IDS.sourceName}
              hint="How this origin appears in the sources list, e.g. acme/widgets."
              error={fieldError(FIELD_IDS.sourceName)}
              required
            >
              <Input
                id={FIELD_IDS.sourceName}
                value={state.newSourceName}
                onChange={(e) => patch({ newSourceName: e.target.value })}
                placeholder="acme/widgets"
                maxLength={128}
              />
            </FormField>
            <FormField
              label="Provider"
              id="wizard-provider"
              hint="Determines how Orbit verifies that a delivery really came from the source."
            >
              <RadioGroup
                id="wizard-provider"
                value={state.newSourceProvider}
                onValueChange={(v) => patch({ newSourceProvider: v, eventType: '' })}
                aria-label="Provider"
              >
                <Grid cols={2} gap="cozy">
                  {PROVIDERS.map((p) => (
                    <RadioCard
                      key={p.id}
                      value={p.id}
                      selected={state.newSourceProvider === p.id}
                      onSelect={(v) => patch({ newSourceProvider: v, eventType: '' })}
                      icon={p.icon}
                      title={p.name}
                      description={p.description}
                    />
                  ))}
                </Grid>
              </RadioGroup>
            </FormField>
          </>
        )}
      </Stack>

      <FormField
        label="Event"
        id="wizard-event"
        hint="A trigger matches one event type. Any event fires on everything the source sends."
      >
        <Stack direction="horizontal">
          <SegmentedControl
            aria-label="Event"
            value={state.eventType}
            onChange={(v) => patch({ eventType: v })}
            options={[
              { value: '', label: 'Any event' },
              ...events.map((ev) => ({ value: ev, label: ev })),
            ]}
          />
        </Stack>
      </FormField>

      <FormField
        label="Prompt"
        id={FIELD_IDS.prompt}
        hint="What the run starts with. Reference the delivery with {{ event.<field> }} placeholders."
        error={fieldError(FIELD_IDS.prompt)}
        required
      >
        <Textarea
          id={FIELD_IDS.prompt}
          value={state.promptTemplate}
          onChange={(e) => patch({ promptTemplate: e.target.value })}
          placeholder="Review the changes in {{ event.repository }} on {{ event.ref }} and comment on anything risky."
          rows={4}
        />
      </FormField>
    </Stack>
  );
}

function validateSourceChoice(state: WizardState): StepValidation {
  if (state.sourceChoice === NEW_SOURCE) {
    if (!state.newSourceName.trim())
      return { valid: false, blocker: 'Needs a source name', fieldId: FIELD_IDS.sourceName };
    return VALID;
  }
  if (!state.sourceChoice)
    return { valid: false, blocker: 'Needs a source', fieldId: FIELD_IDS.source };
  return VALID;
}

function DoneContent({ endpoint, secret }: DoneContentProps) {
  if (!endpoint) return null;
  return (
    <Stack gap="snug">
      <FieldRow
        leading={<Icon name="lock" size="2xs" color="status-healthy" label="" />}
        actions={<CopyButton value={endpoint} label="Copy webhook URL" />}
      >
        <Text as="span" size="caption" mono truncate>
          {endpoint}
        </Text>
      </FieldRow>
      {secret ? (
        <>
          <FieldRow
            leading={
              <Text as="span" size="caption" mono color="subtle">
                secret
              </Text>
            }
            actions={<CopyButton value={secret} label="Copy signing secret" />}
          >
            <Text as="span" size="caption" mono color="muted" truncate>
              {secret}
            </Text>
          </FieldRow>
          <Text size="caption" color="subtle">
            Add this URL and secret to your provider&apos;s webhook settings. The secret is shown
            once.
          </Text>
        </>
      ) : (
        <Text size="caption" color="subtle">
          Add this URL to your provider&apos;s webhook settings. The signing secret is shown once,
          at source creation.
        </Text>
      )}
    </Stack>
  );
}

export const webhookKind: TriggerKindDefinition = {
  id: 'webhook',
  icon: 'webhook',
  title: 'Webhook',
  description: 'An external service posts a signed event to an Orbit endpoint.',
  example: 'GitHub · Alertmanager',
  steps: [
    {
      id: 'source',
      railTitle: 'Source',
      description: 'Where deliveries come from, which event starts a run, and what it asks for.',
      Component: StepSource,
      validate: (state) =>
        firstBlocker(
          () => validateName(state),
          () => validateSourceChoice(state),
          () => validatePrompt(state),
        ),
    },
  ],
  summarize: (state, context) => {
    const source = context.sources.find((s) => s.id === state.sourceChoice);
    return [
      {
        key: 'Source',
        value:
          state.sourceChoice === NEW_SOURCE
            ? `${state.newSourceName || '—'} (new, ${state.newSourceProvider})`
            : (source?.name ?? '—'),
        description: 'Deliveries are accepted only from this source, and only when signed.',
      },
      {
        key: 'Event',
        value: state.eventType || 'Any event',
        mono: Boolean(state.eventType),
        description: 'The event type that starts a run.',
      },
    ];
  },
  draftPatch: (state) => ({
    source:
      state.sourceChoice === NEW_SOURCE
        ? { newName: state.newSourceName.trim(), provider: state.newSourceProvider }
        : { existingId: state.sourceChoice },
    eventType: state.eventType,
  }),
  doneContent: DoneContent,
};
