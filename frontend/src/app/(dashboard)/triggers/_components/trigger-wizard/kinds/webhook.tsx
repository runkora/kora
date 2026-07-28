'use client';

/** @tier 3 — triggers-scoped. The webhook kind: an external system posts a
 * signed event to an Orbit endpoint. Step, review rows, draft, done block. */

import {
  Button,
  FormField,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  Stack,
  Text,
  Textarea,
  FieldRow,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { CopyButton } from '@orbit/components';
import { VALID } from '../wizard-state';
import { NEW_SOURCE } from '../types';
import type {
  DoneContentProps,
  StepComponentProps,
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

function StepSource({ state, patch, context }: StepComponentProps) {
  const { sources, ingestHost } = context;
  const selected = sources.find((s) => s.id === state.sourceChoice);
  const provider =
    state.sourceChoice === NEW_SOURCE ? state.newSourceProvider : (selected?.provider ?? '');
  const events = EVENTS[provider] ?? [];

  return (
    <Stack gap="relaxed">
      <FormField
        label="Trigger name"
        id="wizard-name"
        hint="Shown in the triggers list and on every delivery."
        required
      >
        <Input
          id="wizard-name"
          mono
          value={state.name}
          onChange={(e) => patch({ name: e.target.value })}
          placeholder="github-push"
          maxLength={128}
        />
      </FormField>

      <Stack gap="cozy">
        <SectionLabel>Deliver to</SectionLabel>
        <RadioGroup
          value={state.sourceChoice}
          onValueChange={(v) => patch({ sourceChoice: v })}
          aria-label="Webhook source"
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
              selected={state.sourceChoice === NEW_SOURCE}
              onSelect={(v) => patch({ sourceChoice: v })}
              icon="plus"
              title="New source"
              description="Register a new origin and get its signing secret."
            />
          </Grid>
        </RadioGroup>
        <Text size="micro" mono color="subtle" truncate>
          {state.sourceChoice === NEW_SOURCE || !state.sourceChoice
            ? `https://${ingestHost}/api/v1/webhooks/<issued on create>`
            : endpointFor(ingestHost, state.sourceChoice)}
        </Text>
      </Stack>

      {state.sourceChoice === NEW_SOURCE && (
        <Stack gap="cozy">
          <FormField
            label="Source name"
            id="wizard-source-name"
            hint="How this origin appears in the sources list, e.g. acme/widgets."
            required
          >
            <Input
              id="wizard-source-name"
              value={state.newSourceName}
              onChange={(e) => patch({ newSourceName: e.target.value })}
              placeholder="acme/widgets"
              maxLength={128}
            />
          </FormField>
          <Stack gap="snug">
            <SectionLabel>Provider</SectionLabel>
            <RadioGroup
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
          </Stack>
        </Stack>
      )}

      <Stack gap="snug">
        <SectionLabel>Event</SectionLabel>
        <Text size="caption" color="subtle">
          A trigger matches one event type. Leave it on any event to fire on everything the source
          sends.
        </Text>
        <Stack direction="horizontal" gap="snug" wrap>
          <Button
            size="xs"
            shape="pill"
            variant={state.eventType === '' ? 'accent-outline' : 'outline-muted'}
            aria-pressed={state.eventType === ''}
            onClick={() => patch({ eventType: '' })}
          >
            any event
          </Button>
          {events.map((ev) => (
            <Button
              key={ev}
              size="xs"
              shape="pill"
              variant={state.eventType === ev ? 'accent-outline' : 'outline-muted'}
              aria-pressed={state.eventType === ev}
              onClick={() => patch({ eventType: ev })}
            >
              {ev}
            </Button>
          ))}
        </Stack>
      </Stack>

      <FormField
        label="Prompt"
        id="wizard-prompt"
        hint="What the run starts with. Reference the delivery with {{ event.<field> }} placeholders."
        required
      >
        <Textarea
          id="wizard-prompt"
          value={state.promptTemplate}
          onChange={(e) => patch({ promptTemplate: e.target.value })}
          placeholder="Review the changes in {{ event.repository }} on {{ event.ref }} and comment on anything risky."
          rows={4}
        />
      </FormField>
    </Stack>
  );
}

function validateSource(state: WizardState) {
  if (!state.name.trim()) return { valid: false, hint: 'Give the trigger a name.' };
  if (state.sourceChoice === NEW_SOURCE) {
    if (!state.newSourceName.trim())
      return { valid: false, hint: 'Name the source you are creating.' };
    if (!state.newSourceProvider) return { valid: false, hint: 'Choose a provider.' };
  } else if (!state.sourceChoice) {
    return { valid: false, hint: 'Choose where deliveries come from.' };
  }
  if (!state.promptTemplate.trim())
    return { valid: false, hint: 'Write the prompt the run starts with.' };
  return VALID;
}

function DoneContent({ context, endpoint, secret }: DoneContentProps) {
  if (!endpoint) return null;
  return (
    <Stack gap="snug" minWidth="prose" maxWidth="prose">
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
          <Text size="caption" color="subtle" align="center">
            Add this URL and secret to your provider&apos;s webhook settings. The secret is shown
            once.
          </Text>
        </>
      ) : (
        <Text size="caption" color="subtle" align="center">
          Add this URL to your provider&apos;s webhook settings on {context.ingestHost}. The signing
          secret was shown when the source was created.
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
      railSub: 'Where it comes from',
      description:
        'Choose the source that posts to Orbit, which of its events starts a run, and what the agent is asked to do.',
      Component: StepSource,
      validate: validateSource,
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
      },
      { key: 'Event', value: state.eventType || 'any event', mono: true },
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
