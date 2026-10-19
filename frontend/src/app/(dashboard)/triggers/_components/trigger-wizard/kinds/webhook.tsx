'use client';

/** @tier 3 — triggers-scoped. The webhook kind: an external system posts a
 * signed event to a kora endpoint. Step, review rows, draft, done block. */

import {
  CopyButton,
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
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import {
  describeMatchCondition,
  matchConditionFrom,
  matchContainsFrom,
  coalesceDraftOf,
  coalesceFrom,
  senderRestrictionFrom,
  splitLogins,
  validateFilters,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../shared-fields';
import { eventTypeCatalog, schemaCatalog } from '../field-catalog';
import { CatalogPicker } from '../../catalog-picker';
import { DataBlockPreview } from '../../data-block-preview';
import { FilterRowsEditor } from '../../filter-rows-editor';
import { SenderRestrictionEditor } from '../../sender-restriction-editor';
import { useSourceCatalog } from '../../use-source-catalog';
import { endpointFor } from '../../../_lib/endpoints';
import { NEW_SOURCE } from '../types';
import type {
  DoneContentProps,
  StepComponentProps,
  TriggerKindDefinition,
  WizardState,
} from '../types';

// A new source or provider has its own events, fields and senders.
const RESET_EVENT = { eventType: '', filters: [], senders: { logins: '', associations: [] } };

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

/** The provider whose schema the step reads: the chosen source's, or the new source's. */
function catalogProvider(state: WizardState, sources: { id: string; provider: string }[]) {
  if (state.sourceChoice === NEW_SOURCE) return state.newSourceProvider;
  return sources.find((s) => s.id === state.sourceChoice)?.provider ?? '';
}

function StepSource({ state, patch, context }: StepComponentProps) {
  const { sources, ingestHost } = context;
  const catalog = useSourceCatalog(
    catalogProvider(state, sources),
    state.sourceChoice === NEW_SOURCE ? '' : state.sourceChoice,
    state.eventType,
  );
  const fields = schemaCatalog(catalog.fields);

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
            The kora endpoint your provider posts events to. Triggers listen on a source; several
            triggers can share one.
          </Text>
        </Stack>
        <RadioGroup
          value={state.sourceChoice}
          onValueChange={(v) => patch({ sourceChoice: v, ...RESET_EVENT })}
          aria-label="Webhook source"
        >
          <Grid cols={2} gap="cozy">
            {sources.map((s) => (
              <RadioCard
                key={s.id}
                value={s.id}
                selected={state.sourceChoice === s.id}
                onSelect={(v) => patch({ sourceChoice: v, ...RESET_EVENT })}
                icon="webhook"
                title={s.name}
                description={s.provider}
              />
            ))}
            <RadioCard
              value={NEW_SOURCE}
              selected={state.sourceChoice === NEW_SOURCE}
              onSelect={(v) => patch({ sourceChoice: v, ...RESET_EVENT })}
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
              onValueChange={(v) => patch({ newSourceProvider: v, ...RESET_EVENT })}
              aria-label="Provider"
            >
              <Grid cols={2} gap="cozy">
                {PROVIDERS.map((p) => (
                  <RadioCard
                    key={p.id}
                    value={p.id}
                    selected={state.newSourceProvider === p.id}
                    onSelect={(v) => patch({ newSourceProvider: v, ...RESET_EVENT })}
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
          The event type this trigger listens for, from the provider&apos;s published schema.
        </Text>
        <CatalogPicker
          aria-label="Event type"
          value={state.eventType}
          placeholder="Choose an event"
          catalog={eventTypeCatalog(catalog.declared, catalog.observed)}
          searchPlaceholder="Search events…"
          onSelect={(eventType) => patch({ eventType, filters: [] })}
        />
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
          fields={fields}
          onChange={(filters) => patch({ filters })}
        />
      </Stack>

      <SenderRestrictionEditor
        available={catalog.associations}
        value={state.senders}
        onChange={(senders) => patch({ senders })}
      />

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. Delivery values follow it in a block marked as untrusted data."
        catalog={fields}
        catalogLabel={
          state.eventType
            ? `Fields on ${state.eventType} deliveries.`
            : 'Choose an event to see its fields.'
        }
        placeholder="Answer the question in {{ comment.body }} on {{ issue.html_url }}."
      />
      <DataBlockPreview template={state.promptTemplate} />
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
    if (!s.eventType) return { valid: false, hint: 'Choose the event this trigger listens for.' };
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
  description: 'An external service posts a signed event to a kora endpoint.',
  example: 'GitHub · Alertmanager',
  steps: [
    {
      id: 'source',
      railTitle: 'Source',
      railSub: 'Where it comes from',
      description:
        'Choose the source that posts to kora, which of its events starts a run, and what the agent is asked to do.',
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
      { key: 'Event', value: state.eventType, mono: true },
    ];
    const filters = describeMatchCondition(state.filters);
    if (filters) rows.push({ key: 'Filters', value: filters, mono: true });
    const senders = [...splitLogins(state.senders.logins), ...state.senders.associations];
    if (senders.length > 0) rows.push({ key: 'Senders', value: senders.join(', '), mono: true });
    return rows;
  },
  draftPatch: (state) => {
    // The match condition is webhook-only, so it is folded here beside the
    // source and event it belongs to rather than in the shared draft builder.
    const matchCondition = matchConditionFrom(state.filters);
    const matchContains = matchContainsFrom(state.filters);
    const senderRestriction = senderRestrictionFrom(state.senders);
    // Coalescing only works with `wait`, so a leftover value is dropped otherwise.
    const coalesce = state.overflow === 'wait' ? coalesceFrom(coalesceDraftOf(state)) : undefined;
    return {
      source:
        state.sourceChoice === NEW_SOURCE
          ? { newName: state.newSourceName.trim(), provider: state.newSourceProvider }
          : { existingId: state.sourceChoice },
      eventType: state.eventType,
      ...(matchCondition ? { matchCondition } : {}),
      ...(matchContains ? { matchContains } : {}),
      ...(senderRestriction ? { senderRestriction } : {}),
      ...(coalesce ? { coalesce } : {}),
    };
  },
  doneContent: DoneContent,
};
