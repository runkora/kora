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
  FieldRow,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { CopyButton } from '@orbit/components';
import {
  describeMatchCondition,
  matchConditionFrom,
  validateFilters,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../shared-fields';
import { catalogPatterns, webhookCatalog, toTemplateCatalog } from '../field-catalog';
import { FilterRowsEditor } from '../../filter-rows-editor';
import { PROVIDER_EVENTS } from '../../../_types';
import { endpointFor } from '../../../_lib/endpoints';
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

function StepSource({ state, patch, context }: StepComponentProps) {
  const { sources, ingestHost } = context;
  const selected = sources.find((s) => s.id === state.sourceChoice);
  const provider =
    state.sourceChoice === NEW_SOURCE ? state.newSourceProvider : (selected?.provider ?? '');
  const events = PROVIDER_EVENTS[provider] ?? [];
  const catalog = webhookCatalog(provider, state.eventType);

  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint="Shown in the triggers list and on every delivery."
        placeholder="github-push"
      />

      <Stack gap="cozy">
        <Stack gap="tight">
          <SectionLabel>Deliver to</SectionLabel>
          <Text size="caption" color="subtle">
            The Orbit endpoint your provider posts events to. Triggers listen on a source; several
            triggers can share one.
          </Text>
        </Stack>
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

      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Filters</SectionLabel>
          <Text size="caption" color="subtle">
            Only deliveries matching every filter start a run. Values are alternatives.
          </Text>
        </Stack>

        <FilterRowsEditor
          rows={state.filters}
          fields={catalog}
          onChange={(filters) => patch({ filters })}
        />
      </Stack>

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. Fields are filled from the delivery that fires it."
        catalog={toTemplateCatalog(catalog, catalogPatterns(provider))}
        catalogLabel={
          state.eventType
            ? `Fields on ${state.eventType} deliveries from this source.`
            : 'Fields across every event this source sends; per-event fields are empty on other events.'
        }
        placeholder="Review {{ pr.title }} on {{ repo.full_name }} and comment on anything risky."
      />
    </Stack>
  );
}

function validateSource(state: WizardState) {
  return validateNamedPromptStep(state, (s) => {
    if (s.sourceChoice === NEW_SOURCE) {
      if (!s.newSourceName.trim())
        return { valid: false, hint: 'Name the source you are creating.' };
      if (!s.newSourceProvider) return { valid: false, hint: 'Choose a provider.' };
    } else if (!s.sourceChoice) {
      return { valid: false, hint: 'Choose where deliveries come from.' };
    }
    return validateFilters(s.filters);
  });
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
      <FieldRow
        leading={
          <Text as="span" size="caption" mono color="subtle">
            content type
          </Text>
        }
        actions={<CopyButton value="application/json" label="Copy content type" />}
      >
        <Text as="span" size="caption" mono color="muted" truncate>
          application/json
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
            Add this URL, content type and secret to your provider&apos;s webhook settings. The
            secret is shown once.
          </Text>
        </>
      ) : (
        <Text size="caption" color="subtle" align="center">
          Add this URL and content type to your provider&apos;s webhook settings on{' '}
          {context.ingestHost}. The signing secret was shown when the source was created.
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
    const rows = [
      {
        key: 'Source',
        value:
          state.sourceChoice === NEW_SOURCE
            ? `${state.newSourceName || '—'} (new, ${state.newSourceProvider})`
            : (source?.name ?? '—'),
      },
      { key: 'Event', value: state.eventType || 'any event', mono: true },
    ];
    const filters = describeMatchCondition(state.filters);
    if (filters) rows.push({ key: 'Filters', value: filters, mono: true });
    return rows;
  },
  fieldCatalog: (state, context) => {
    const source = context.sources.find((s) => s.id === state.sourceChoice);
    const provider =
      state.sourceChoice === NEW_SOURCE ? state.newSourceProvider : (source?.provider ?? '');
    return webhookCatalog(provider, state.eventType);
  },
  draftPatch: (state) => {
    // The match condition is webhook-only, so it is folded here beside the
    // source and event it belongs to rather than in the shared draft builder.
    const matchCondition = matchConditionFrom(state.filters);
    return {
      source:
        state.sourceChoice === NEW_SOURCE
          ? { newName: state.newSourceName.trim(), provider: state.newSourceProvider }
          : { existingId: state.sourceChoice },
      eventType: state.eventType,
      ...(matchCondition ? { matchCondition } : {}),
    };
  },
  doneContent: DoneContent,
};
