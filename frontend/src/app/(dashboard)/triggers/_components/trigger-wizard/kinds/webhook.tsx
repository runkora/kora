'use client';

import { useRouter } from 'next/navigation';
import { useEffect, useState } from 'react';

/** @tier 3 · triggers-scoped. The webhook and chat command kinds: an external
 * system posts a signed event to a kora endpoint. One module, two registry
 * entries told apart by the source category the API reports. Step, review
 * rows, draft, done block. */

import {
  Alert,
  Button,
  CodeBlock,
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
import { useBrand } from '@runkora/theme/brand-provider';
import type { SourceCategory } from '../../../_types';
import {
  describeMatchCondition,
  matchConditionFrom,
  matchContainsFrom,
  coalesceDraftOf,
  coalesceFrom,
  senderRestrictionFrom,
  splitIds,
  unknownFieldHint,
  VALID,
  validateFilters,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../../trigger-fields';
import { schemaCatalog } from '../field-catalog';
import { EventFiltersSection } from '../../event-filters-section';
import { DataBlockPreview } from '../../data-block-preview';
import { SenderRestrictionEditor } from '../../sender-restriction-editor';
import { useProviderCategories, useSourceCatalog } from '../../use-source-catalog';
import { endpointFor } from '../../../_lib/endpoints';
import { createSource, updateSource } from '../../../_services/triggers-api';
import {
  chatConversationKey,
  chatEventLabel,
  firstUnknownChatField,
  LATER_CHAT_PLATFORMS,
  placeholderKeys,
} from '../../../_lib/chat';
import { PROVIDER_IDS, PROVIDERS, providerName, SENDER_ID_EXAMPLE } from '../../../_lib/providers';
import { SLACK_PROVIDER, slackAppManifest } from '../../../_lib/slack-app-manifest';
import {
  defaultEventType,
  fixedEventType,
  GENERIC_EVENT_TYPE,
  GENERIC_PROVIDER,
  genericConfigFrom,
  genericDraftHint,
  pastesSecret,
  VERIFICATION_METHODS,
} from '../../../_lib/generic-source';
import { GenericSourceSetup } from './generic-source-setup';
import { NEW_SOURCE } from '../types';
import type {
  DoneContentProps,
  StepComponentProps,
  TriggerKindDefinition,
  WizardContext,
  WizardState,
} from '../types';

// A new source or provider has its own events, fields and senders.
const RESET_EVENT = {
  eventType: '',
  eventFields: null,
  eventFieldsFailed: false,
  filters: [],
  senders: { ids: '', roles: [] },
};

/** The copy that differs between the two kinds. */
interface SourceKindCopy {
  namePlaceholder: string;
  nameHint: string;
  sourceLabel: string;
  sourceHelp: (productName: string) => string;
  newSourceHelp: string;
  sourceNameHint: string;
  sourceNamePlaceholder: string;
  providerLabel: string;
  filtersHelp: string;
  promptPlaceholder: string;
  /** What arrives on the source, in prompt and validation copy. */
  arrivals: string;
}

// Example prompt for sources with no schema. It uses the meta fields every delivery carries.
const GENERIC_PROMPT_PLACEHOLDER =
  'Handle the {{ meta.event_type }} delivery from {{ meta.source }}.';

const COPY: Record<SourceCategory, SourceKindCopy> = {
  webhook: {
    namePlaceholder: 'github-push',
    nameHint: 'Shown in the triggers list and on every delivery.',
    sourceLabel: 'Webhook source',
    sourceHelp: (productName) =>
      `The ${productName} endpoint your provider posts events to. Triggers listen on a source; several triggers can share one.`,
    newSourceHelp: 'Register a new origin that posts to this endpoint.',
    sourceNameHint: 'How this origin appears in the sources list, e.g. example-org/widgets.',
    sourceNamePlaceholder: 'example-org/widgets',
    providerLabel: 'Provider',
    filtersHelp: 'Only deliveries matching every filter start a run. Values are alternatives.',
    promptPlaceholder: 'Answer the question in {{ comment.body }} on {{ issue.html_url }}.',
    arrivals: 'deliveries',
  },
  chat: {
    namePlaceholder: 'answer-mentions',
    nameHint: 'Shown in the triggers list and on every message it receives.',
    sourceLabel: 'Chat source',
    sourceHelp: (productName) =>
      `The chat app that sends messages to ${productName}. Triggers listen on a source; several triggers can share one.`,
    newSourceHelp: 'Connect a chat workspace through a new app.',
    sourceNameHint: 'How this workspace appears in the sources list, e.g. example-slack.',
    sourceNamePlaceholder: 'example-slack',
    providerLabel: 'Platform',
    filtersHelp:
      'Only messages matching every filter start a run. A filter on conversation.id limits it to chosen channels. Values are alternatives.',
    promptPlaceholder:
      'Answer {{ text }} from {{ user.id }}. Reply in thread {{ thread.id }} of conversation {{ conversation.id }}.',
    arrivals: 'messages',
  },
};

/** The provider whose schema the step reads: the chosen source's, or the new source's. */
function catalogProvider(state: WizardState, sources: { id: string; provider: string }[]) {
  if (state.sourceChoice === NEW_SOURCE) return state.newSourceProvider;
  return sources.find((s) => s.id === state.sourceChoice)?.provider ?? '';
}

/** The sources a kind lists: those of its category. */
function ownSources(context: WizardContext, category: SourceCategory) {
  return context.sources.filter((s) => s.category === category);
}

/**
 * The event type a trigger starts with on a source: `webhook` for a generic
 * source that has no event type location, since every delivery carries it.
 */
function startingEventType(
  state: WizardState,
  sources: { id: string; provider: string; config?: Record<string, unknown> }[],
  sourceChoice: string,
  provider: string,
): string {
  if (sourceChoice === NEW_SOURCE)
    return provider === GENERIC_PROVIDER
      ? defaultEventType(state.newSourceGeneric.eventLocation)
      : '';
  const source = sources.find((s) => s.id === sourceChoice);
  return source === undefined ? '' : (fixedEventType(source.provider, source.config) ?? '');
}

function categoryOf(definition: TriggerKindDefinition): SourceCategory {
  return definition.sourceCategory ?? 'webhook';
}

function StepSource({ state, patch, context, definition }: StepComponentProps) {
  const { ingestHost } = context;
  const category = categoryOf(definition);
  const copy = COPY[category];
  const sources = ownSources(context, category);
  const { productName } = useBrand();
  const providerCategories = useProviderCategories(PROVIDER_IDS);
  const providers = PROVIDERS.filter((p) => providerCategories.categories[p.id] === category);
  const catalog = useSourceCatalog(
    catalogProvider(state, sources),
    state.sourceChoice === NEW_SOURCE ? '' : state.sourceChoice,
    state.eventType,
  );
  const fields = schemaCatalog(catalog.fields);
  // A generic source has no schema: event types and field paths are typed.
  const schemaless = category === 'webhook' && catalog.schemaless;
  const chooseSource = (sourceChoice: string) =>
    patch({
      sourceChoice,
      ...RESET_EVENT,
      eventType: startingEventType(state, sources, sourceChoice, state.newSourceProvider),
    });
  const chooseProvider = (provider: string) =>
    patch({
      newSourceProvider: provider,
      newSourceSecret: '',
      ...RESET_EVENT,
      eventType: startingEventType(state, sources, NEW_SOURCE, provider),
    });
  const loadedPaths = catalog.fieldsReady ? catalog.fields.map((f) => f.path).join('\n') : null;

  // A chat trigger's fields are checked before create, since the backend
  // refuses any other field once a new source may already exist.
  const fieldsFailed = state.eventType !== '' && catalog.error !== null;
  useEffect(() => {
    if (category !== 'chat') return;
    if (loadedPaths !== null)
      patch({ eventFields: loadedPaths.split('\n'), eventFieldsFailed: false });
    else if (fieldsFailed) patch({ eventFieldsFailed: true });
  }, [category, loadedPaths, fieldsFailed, patch]);

  // With one platform to pick, picking it is not a decision worth asking for.
  const onlyProvider =
    providerCategories.pending.length === 0 && providers.length === 1
      ? providers[0]?.id
      : undefined;
  useEffect(() => {
    if (
      category === 'chat' &&
      onlyProvider !== undefined &&
      state.sourceChoice === NEW_SOURCE &&
      !state.newSourceProvider
    )
      patch({ newSourceProvider: onlyProvider });
  }, [category, onlyProvider, state.sourceChoice, state.newSourceProvider, patch]);

  const eventName = category === 'chat' ? chatEventLabel(state.eventType) : state.eventType;

  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint={copy.nameHint}
        placeholder={copy.namePlaceholder}
      />

      <Stack gap="cozy">
        <Stack gap="tight">
          <SectionLabel>Deliver to</SectionLabel>
          <Text size="caption" color="subtle">
            {copy.sourceHelp(productName)}
          </Text>
        </Stack>
        <RadioGroup
          value={state.sourceChoice}
          onValueChange={chooseSource}
          aria-label={copy.sourceLabel}
        >
          <Grid cols={2} gap="cozy">
            {sources.map((s) => (
              <RadioCard
                key={s.id}
                value={s.id}
                selected={state.sourceChoice === s.id}
                onSelect={chooseSource}
                icon={definition.icon}
                title={s.name}
                description={
                  s.signingSecretSet === false
                    ? `${providerName(s.provider)} · waiting for signing secret`
                    : providerName(s.provider)
                }
              />
            ))}
            <RadioCard
              value={NEW_SOURCE}
              selected={state.sourceChoice === NEW_SOURCE}
              onSelect={chooseSource}
              icon="plus"
              title="New source"
              description={copy.newSourceHelp}
            />
          </Grid>
        </RadioGroup>
        {/* A source that does not exist yet has no request URL to show. */}
        {state.sourceChoice !== NEW_SOURCE && state.sourceChoice !== '' && (
          <Text size="micro" mono color="subtle" truncate>
            {endpointFor(ingestHost, state.sourceChoice)}
          </Text>
        )}
      </Stack>

      {state.sourceChoice === NEW_SOURCE && (
        <Stack gap="cozy">
          <FormField
            label="Source name"
            id="wizard-source-name"
            hint={copy.sourceNameHint}
            required
          >
            <Input
              id="wizard-source-name"
              value={state.newSourceName}
              onChange={(e) => patch({ newSourceName: e.target.value })}
              disabled={state.newSourceCreated !== null}
              placeholder={copy.sourceNamePlaceholder}
              maxLength={128}
            />
          </FormField>
          <Stack gap="snug">
            <SectionLabel>{copy.providerLabel}</SectionLabel>
            <RadioGroup
              value={state.newSourceProvider}
              onValueChange={chooseProvider}
              aria-label={copy.providerLabel}
              disabled={state.newSourceCreated !== null}
            >
              <Grid cols={category === 'chat' ? 3 : 2} gap="cozy">
                {providers.map((p) => (
                  <RadioCard
                    key={p.id}
                    value={p.id}
                    selected={state.newSourceProvider === p.id}
                    onSelect={chooseProvider}
                    icon={p.icon}
                    title={p.name}
                    description={p.description}
                  />
                ))}
                {/* Planned platforms, disabled like the planned trigger kinds. */}
                {category === 'chat' &&
                  LATER_CHAT_PLATFORMS.map((p) => (
                    <RadioCard
                      key={p.name}
                      value={p.name}
                      selected={false}
                      disabled
                      onSelect={() => undefined}
                      icon={p.icon}
                      title={p.name}
                      description="Planned."
                    />
                  ))}
              </Grid>
            </RadioGroup>
            {providerCategories.failed.length > 0 ? (
              <Text size="caption" color="status-error">
                Could not load {providerCategories.failed.map(providerName).join(', ')}. Close the
                wizard and open it again to retry.
              </Text>
            ) : (
              providers.length === 0 &&
              providerCategories.pending.length > 0 && (
                <Text size="caption" color="subtle">
                  Loading…
                </Text>
              )
            )}
          </Stack>
          {state.newSourceProvider === SLACK_PROVIDER && (
            <SlackAppSetup state={state} patch={patch} ingestHost={ingestHost} />
          )}
          {state.newSourceProvider === GENERIC_PROVIDER && (
            <GenericSourceSetup
              draft={state.newSourceGeneric}
              onChange={(next) =>
                patch({
                  newSourceGeneric: next,
                  ...(next.eventLocation !== state.newSourceGeneric.eventLocation && {
                    eventType: defaultEventType(next.eventLocation),
                  }),
                })
              }
              secret={state.newSourceSecret}
              onSecretChange={(newSourceSecret) => patch({ newSourceSecret })}
            />
          )}
        </Stack>
      )}

      <EventFiltersSection
        title="Event filters"
        category={category}
        catalog={catalog}
        eventType={state.eventType}
        filters={state.filters}
        schemaless={schemaless}
        eventHelp={
          category === 'chat'
            ? 'The message that starts a run. Messages from bots and apps never do.'
            : schemaless
              ? 'The event type this trigger listens for, as the sender names it. Types this source has received are suggested.'
              : "The event type this trigger listens for, from the provider's published schema."
        }
        filtersHelp={copy.filtersHelp}
        onEventTypeChange={(eventType) => {
          if (category === 'chat') {
            // Picking the chosen event again keeps its loaded fields.
            if (eventType === state.eventType) return;
            patch({ eventType, eventFields: null, eventFieldsFailed: false, filters: [] });
          } else {
            // Typing keeps the filters: a schemaless source's fields do not depend on the event.
            patch(schemaless ? { eventType } : { eventType, filters: [] });
          }
        }}
        onFiltersChange={(filters) => patch({ filters })}
      />

      <SenderRestrictionEditor
        identifiesSenders={catalog.identifiesSenders}
        roles={catalog.roles}
        value={state.senders}
        onChange={(senders) => patch({ senders })}
        idPlaceholder={SENDER_ID_EXAMPLE[category]}
      />

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. Delivery values follow it in a block marked as untrusted data."
        catalog={fields}
        catalogLabel={
          schemaless
            ? 'Delivery metadata. Type any payload path, such as {{ data.issue.title }}.'
            : state.eventType
              ? `Fields on ${eventName} ${copy.arrivals}.`
              : 'Choose an event to see its fields.'
        }
        placeholder={schemaless ? GENERIC_PROMPT_PLACEHOLDER : copy.promptPlaceholder}
      />
      <DataBlockPreview template={state.promptTemplate} />
    </Stack>
  );
}

function validateSource(category: SourceCategory) {
  return (state: WizardState) =>
    validateNamedPromptStep(state, (s) => {
      if (s.sourceChoice === NEW_SOURCE) {
        if (!s.newSourceName.trim())
          return { valid: false, hint: 'Name the source you are creating.' };
        if (!s.newSourceProvider)
          return {
            valid: false,
            hint: category === 'chat' ? 'Choose a platform.' : 'Choose a provider.',
          };
        if (s.newSourceProvider === GENERIC_PROVIDER) {
          const hint = genericDraftHint(s.newSourceGeneric, s.newSourceSecret);
          if (hint !== undefined) return { valid: false, hint };
        }
        if (s.newSourceProvider === SLACK_PROVIDER) {
          if (s.newSourceCreated === null)
            return { valid: false, hint: 'Create the source to get its request URL.' };
          if (!s.newSourceSecretSaved)
            return { valid: false, hint: 'Save the signing secret from your Slack app.' };
        }
      } else if (!s.sourceChoice) {
        return { valid: false, hint: `Choose where ${COPY[category].arrivals} come from.` };
      }
      if (!s.eventType.trim())
        return {
          valid: false,
          hint:
            category === 'chat'
              ? 'Choose the message this trigger listens for.'
              : 'Choose the event this trigger listens for.',
        };
      const filters = validateFilters(s.filters);
      if (!filters.valid || category !== 'chat') return filters;
      return validateChatFields(s);
    });
}

/**
 * Every field a chat trigger names on its source step: prompt placeholders
 * and filter fields. The backend refuses anything else at create.
 */
function validateChatFields(s: WizardState) {
  if (s.eventFields === null)
    return {
      valid: false,
      hint: s.eventFieldsFailed
        ? 'Could not load the message fields. Close the wizard and open it again to retry.'
        : 'Loading the message fields…',
    };
  const keys = [
    ...placeholderKeys(s.promptTemplate),
    ...s.filters.map((row) => row.field.trim()).filter(Boolean),
  ];
  const unknown = firstUnknownChatField(keys, s.eventFields);
  return unknown === undefined ? VALID : { valid: false, hint: unknownFieldHint(unknown) };
}

/**
 * A new Slack source's setup, in the order Slack needs. The source is created
 * first, with no signing secret, so its request URL exists. The admin sets the
 * URL in Slack, which checks it, and then saves the signing secret Slack issued.
 */
function SlackAppSetup({
  state,
  patch,
  ingestHost,
}: {
  state: WizardState;
  patch: (next: Partial<WizardState>) => void;
  ingestHost: string;
}) {
  const { productName } = useBrand();
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const created = state.newSourceCreated;

  const create = async () => {
    setPending(true);
    setError(null);
    const name = state.newSourceName.trim();
    const made = await createSource({ name, provider: SLACK_PROVIDER });
    setPending(false);
    if (made.error !== null || made.source === null) {
      setError(made.error ?? 'Failed to create the source.');
      return;
    }
    // The source exists now; refresh so a wizard closed early leaves it in the
    // source picker rather than orphaned, where a duplicate could be created.
    router.refresh();
    patch({
      newSourceCreated: { id: made.source.id, name, provider: SLACK_PROVIDER },
      newSourceSecretSaved: false,
    });
  };

  const saveSecret = async () => {
    if (created === null) return;
    setPending(true);
    setError(null);
    const saved = await updateSource(created.id, { signing_secret: state.newSourceSecret.trim() });
    setPending(false);
    if (saved.error !== null) {
      setError(saved.error);
      return;
    }
    patch({ newSourceSecretSaved: true, newSourceSecret: '' });
  };

  if (created === null) {
    return (
      <Stack gap="cozy">
        <Stack gap="tight">
          <SectionLabel>Slack app</SectionLabel>
          <Text size="caption" color="subtle">
            Create the source first. Slack needs its request URL to finish setting up events.
          </Text>
        </Stack>
        {error !== null && <Alert variant="error">{error}</Alert>}
        <Stack direction="horizontal" justify="end">
          <Button
            size="xs"
            disabled={pending || !state.newSourceName.trim()}
            onClick={() => void create()}
          >
            {pending ? 'Creating…' : 'Create source'}
          </Button>
        </Stack>
      </Stack>
    );
  }

  const requestUrl = endpointFor(ingestHost, created.id);
  return (
    <Stack gap="cozy">
      <Stack gap="tight">
        <SectionLabel>Slack app</SectionLabel>
        <Text size="caption" color="subtle">
          Create a Slack app from this manifest. Its request URL is already set. For an existing
          app, set the request URL below under Event Subscriptions instead. Then paste the signing
          secret from the app&apos;s Basic Information page.
        </Text>
      </Stack>
      <CodeBlock
        text={JSON.stringify(slackAppManifest(productName, requestUrl), null, 2)}
        filename="manifest.json"
        language="json"
        density="compact"
      />
      <FieldRow
        leading={
          <Text as="span" size="caption" mono color="subtle">
            request URL
          </Text>
        }
        actions={<CopyButton value={requestUrl} label="Copy request URL" />}
      >
        <Text as="span" size="caption" mono truncate>
          {requestUrl}
        </Text>
      </FieldRow>
      {state.newSourceSecretSaved ? (
        <Text size="caption" color="status-healthy">
          Signing secret saved.
        </Text>
      ) : (
        <Stack gap="snug">
          <FormField label="Signing secret" id="wizard-source-secret" required>
            <Input
              id="wizard-source-secret"
              type="password"
              autoComplete="off"
              mono
              value={state.newSourceSecret}
              disabled={pending}
              onChange={(e) => patch({ newSourceSecret: e.target.value })}
              placeholder="From Basic Information in your Slack app"
              maxLength={256}
            />
          </FormField>
          {error !== null && <Alert variant="error">{error}</Alert>}
          <Stack direction="horizontal" justify="end">
            <Button
              size="xs"
              disabled={pending || !state.newSourceSecret.trim()}
              onClick={() => void saveSecret()}
            >
              {pending ? 'Saving…' : 'Save secret'}
            </Button>
          </Stack>
        </Stack>
      )}
    </Stack>
  );
}

/** The chat command kind's done block: the request URL the chat app sends events to. */
function ChatDoneContent({ state, context, endpoint }: DoneContentProps) {
  if (!endpoint) return null;
  const provider = catalogProvider(state, context.sources);
  const isNew = state.sourceChoice === NEW_SOURCE;
  const note =
    provider === SLACK_PROVIDER
      ? isNew
        ? 'This source’s request URL is set up in your Slack app. Install the app and invite the bot to the channels this trigger should hear.'
        : 'This source’s request URL, set under Event Subscriptions in its Slack app. Invite the bot to the channels this trigger should hear.'
      : `Set this request URL in your ${providerName(provider)} app’s event settings.`;
  return (
    <Stack gap="snug" minWidth="prose" maxWidth="prose">
      <FieldRow
        leading={
          <Text as="span" size="caption" mono color="subtle">
            request URL
          </Text>
        }
        actions={<CopyButton value={endpoint} label="Copy request URL" />}
      >
        <Text as="span" size="caption" mono truncate>
          {endpoint}
        </Text>
      </FieldRow>
      <Text size="caption" color="subtle" align="center">
        {note}
      </Text>
    </Stack>
  );
}

function WebhookDoneContent({ state, context, endpoint, secret }: DoneContentProps) {
  if (!endpoint) return null;
  const pasted =
    state.sourceChoice === NEW_SOURCE &&
    state.newSourceProvider === GENERIC_PROVIDER &&
    pastesSecret(state.newSourceGeneric);
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
          {pasted
            ? 'Add this URL and content type to your sender’s webhook settings. Deliveries are verified with the secret you pasted.'
            : `Add this URL and content type to your provider’s webhook settings on ${context.ingestHost}. The signing secret was shown when the source was created.`}
        </Text>
      )}
    </Stack>
  );
}

/** The source step, review rows and draft both kinds share; they differ in their category. */
function sourceKind(
  category: SourceCategory,
): Pick<
  TriggerKindDefinition,
  'apiKind' | 'sourceCategory' | 'steps' | 'summarize' | 'draftPatch'
> {
  return {
    apiKind: 'webhook',
    sourceCategory: category,
    steps: [
      {
        id: 'source',
        railTitle: 'Source',
        railSub: category === 'chat' ? 'Where messages come from' : 'Where it comes from',
        description: (productName) =>
          category === 'chat'
            ? `Choose the chat app that sends messages to ${productName}, which message starts a run, and what the agent is asked to do.`
            : `Choose the source that posts to ${productName}, which of its events starts a run, and what the agent is asked to do.`,
        Component: StepSource,
        validate: validateSource(category),
      },
    ],
    summarize: (state, context) => {
      const source = context.sources.find((s) => s.id === state.sourceChoice);
      const rows = [
        {
          key: 'Source',
          value:
            state.sourceChoice === NEW_SOURCE
              ? `${state.newSourceName || '—'} (new, ${providerName(state.newSourceProvider)})`
              : (source?.name ?? '—'),
        },
        category === 'chat'
          ? { key: 'Event', value: chatEventLabel(state.eventType) }
          : { key: 'Event', value: state.eventType, mono: true },
      ];
      if (state.sourceChoice === NEW_SOURCE && state.newSourceProvider === GENERIC_PROVIDER) {
        const method = VERIFICATION_METHODS.find((m) => m.value === state.newSourceGeneric.method);
        rows.push({
          key: 'Verification',
          value: `${method?.title ?? ''}, ${pastesSecret(state.newSourceGeneric) ? 'pasted secret' : 'generated secret'}`,
        });
      }
      const filters = describeMatchCondition(state.filters);
      if (filters) rows.push({ key: 'Filters', value: filters, mono: true });
      const senders = [...splitIds(state.senders.ids), ...state.senders.roles];
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
          state.sourceChoice !== NEW_SOURCE
            ? { existingId: state.sourceChoice }
            : state.newSourceCreated !== null
              ? // The source step already created it.
                { existingId: state.newSourceCreated.id }
              : {
                  newName: state.newSourceName.trim(),
                  provider: state.newSourceProvider,
                  ...(state.newSourceProvider === GENERIC_PROVIDER && {
                    config: genericConfigFrom(state.newSourceGeneric),
                    ...(pastesSecret(state.newSourceGeneric) && {
                      signingSecret: state.newSourceSecret.trim(),
                    }),
                  }),
                },
        eventType: state.eventType.trim(),
        ...(matchCondition ? { matchCondition } : {}),
        ...(matchContains ? { matchContains } : {}),
        ...(senderRestriction ? { senderRestriction } : {}),
        ...(coalesce ? { coalesce } : {}),
      };
    },
  };
}

export const webhookKind: TriggerKindDefinition = {
  id: 'webhook',
  icon: 'webhook',
  title: 'Webhook',
  description: (productName) =>
    `An external service posts a signed event to your ${productName} endpoint.`,
  example: 'GitHub · Alertmanager · Generic',
  ...sourceKind('webhook'),
  doneContent: WebhookDoneContent,
};

/**
 * A chat command trigger is stored as a webhook trigger on a chat source, so
 * this entry reuses the webhook module and lists only chat sources.
 */
export const chatKind: TriggerKindDefinition = {
  id: 'chat',
  icon: 'send',
  title: 'Chat command',
  description: 'Someone mentions the bot in a channel or sends it a direct message.',
  example: 'Slack',
  defaults: { newSourceProvider: '' },
  ...sourceKind('chat'),
  doneContent: ChatDoneContent,
  coalesceKeyExample: chatConversationKey,
  newSourceWhenNone: true,
};
