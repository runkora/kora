'use client';

/** @tier 3 — triggers-scoped. Shared Target step: route every delivery to one
 * agent, or let kora pick the agent and the run for each delivery. How runs
 * queue belongs with the rest of the queue, on Delivery. */

import { useState } from 'react';
import {
  Button,
  Callout,
  Card,
  CardContent,
  Checkbox,
  DataTable,
  FormField,
  Grid,
  Icon,
  IconTile,
  Input,
  NativeSelect,
  RadioCard,
  RadioGroup,
  RadioGroupItem,
  SectionLabel,
  Stack,
  Text,
  ToggleRow,
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import type { TriggerRoutingConfidence } from '@runkora/shared/api';
import { needsDescription, noDecisionModels } from './wizard-state';
import type {
  DecisionModel,
  RoutingAgent,
  RoutingDraft,
  RoutingMode,
  RoutingOptions,
  StepComponentProps,
  TriggerWizardAgent,
  UnsureAction,
} from './types';
import { permissionHintText } from '@runkora/components';

function initial(agent: TriggerWizardAgent): string {
  const source = agent.name?.trim() || agent.slug;
  return (source.charAt(0) || '?').toUpperCase();
}

function agentLabel(agent: TriggerWizardAgent): string {
  return agent.name?.trim() || agent.slug;
}

/** Above this many agents the list gains a search box and pages. */
const AGENT_SEARCH_THRESHOLD = 6;

/** Agents shown per page: enough to scan, few enough to never scroll. */
const AGENT_PAGE_SIZE = 5;

function confidenceOptions(productName: string): {
  id: TriggerRoutingConfidence;
  title: string;
  description: string;
}[] {
  return [
    {
      id: 'strict',
      title: 'Strict',
      description: `Only routes when ${productName} is sure. More deliveries go to the fallback.`,
    },
    {
      id: 'balanced',
      title: 'Balanced (default)',
      description: `Routes when ${productName} is fairly sure. Suits most triggers.`,
    },
    {
      id: 'loose',
      title: 'Loose',
      description: `Routes on ${productName}’s best guess. Fewer fallbacks, more wrong picks.`,
    },
  ];
}

export function StepTarget(props: StepComponentProps) {
  const { state, patch, context, definition } = props;
  const { productName } = useBrand();
  const options = context.routing;
  if (options === undefined) return <AgentPicker {...props} />;
  const routing = state.routing;
  const onRouting = (next: Partial<RoutingDraft>) => patch({ routing: { ...routing, ...next } });
  return (
    <Stack gap="relaxed">
      <RadioGroup
        value={routing.mode}
        onValueChange={(v) => onRouting({ mode: v as RoutingMode })}
        aria-label="How deliveries reach an agent"
      >
        <Grid cols={2} gap="cozy">
          <RadioCard
            value="one"
            selected={routing.mode === 'one'}
            onSelect={() => onRouting({ mode: 'one' })}
            icon="bot"
            title="Route to one agent"
            description="Every delivery starts a run of the agent you pick."
          />
          <RadioCard
            value="choose"
            selected={routing.mode === 'choose'}
            onSelect={() => onRouting({ mode: 'choose' })}
            icon="route"
            title={`Let ${productName} choose`}
            description={`${productName} picks the agent for each delivery, and whether it starts a run or joins an open one.`}
          />
        </Grid>
      </RadioGroup>
      {routing.mode === 'one' ? (
        <AgentPicker {...props} />
      ) : (
        <ChooseSettings
          routing={routing}
          onRouting={onRouting}
          options={options}
          joinKeyExample={definition.coalesceKeyExample?.(state.eventType)}
        />
      )}
    </Stack>
  );
}

/** Route to one agent: the agent every delivery starts a run of. */
function AgentPicker({ state, patch, context }: StepComponentProps) {
  const { agents } = context;
  const [agentQuery, setAgentQuery] = useState('');
  const [agentPage, setAgentPage] = useState(0);
  const query = agentQuery.trim().toLowerCase();
  const matchingAgents =
    query === ''
      ? agents
      : agents.filter(
          (a) =>
            a.slug.toLowerCase().includes(query) || (a.name ?? '').toLowerCase().includes(query),
        );
  // Paged, never scrolled: a scroll region inside the step's own scroll was
  // two nested scrollbars, and flexbox clipping besides.
  const pageCount = Math.max(1, Math.ceil(matchingAgents.length / AGENT_PAGE_SIZE));
  const page = Math.min(agentPage, pageCount - 1);
  const visibleAgents = matchingAgents.slice(
    page * AGENT_PAGE_SIZE,
    page * AGENT_PAGE_SIZE + AGENT_PAGE_SIZE,
  );
  return (
    <Stack gap="relaxed">
      <Stack gap="cozy">
        <SectionLabel>Agent to run</SectionLabel>
        {agents.length === 0 && (
          <Callout variant="warning">
            {context.agentsError ??
              'No agents are available to bind. Create an autonomous agent first.'}
          </Callout>
        )}
        {agents.length > AGENT_SEARCH_THRESHOLD && (
          <AgentSearch
            query={agentQuery}
            onQuery={(q) => {
              setAgentQuery(q);
              setAgentPage(0);
            }}
            matching={matchingAgents.length}
            total={agents.length}
          />
        )}
        <RadioGroup
          value={state.agentTypeId}
          onValueChange={(v) => patch({ agentTypeId: v })}
          aria-label="Agent"
        >
          <Stack gap="snug">
            {visibleAgents.length === 0 && (
              <Text size="caption" color="subtle">
                No agents match this search.
              </Text>
            )}
            {visibleAgents.map((a) => {
              const delegated = a.identityMode === 'delegated';
              const unrunnable = a.canRun === false;
              const blocked = delegated || unrunnable;
              return (
                <Card
                  key={a.id}
                  interactive={!blocked}
                  subtle={blocked}
                  selected={state.agentTypeId === a.id}
                  onClick={blocked ? undefined : () => patch({ agentTypeId: a.id })}
                >
                  <CardContent padded compact>
                    <Stack direction="horizontal" align="center" gap="cozy">
                      <IconTile tone={state.agentTypeId === a.id ? 'accent' : 'muted'}>
                        <Text as="span" size="caption" mono weight="heavy">
                          {initial(a)}
                        </Text>
                      </IconTile>
                      <Stack gap="tight" grow minWidth="none">
                        <Text as="span" size="supporting" weight="heavy">
                          {agentLabel(a)}
                        </Text>
                        <Text as="span" size="micro" mono color="subtle">
                          {a.slug}
                        </Text>
                      </Stack>
                      {delegated ? (
                        <Text as="span" size="micro" color="subtle">
                          delegated · started by people
                        </Text>
                      ) : unrunnable ? (
                        <Text as="span" size="micro" color="subtle">
                          {permissionHintText('run this agent')}
                        </Text>
                      ) : (
                        <RadioGroupItem value={a.id} aria-label={agentLabel(a)} />
                      )}
                    </Stack>
                  </CardContent>
                </Card>
              );
            })}
          </Stack>
        </RadioGroup>
        <Pager
          page={page}
          pageCount={pageCount}
          shown={visibleAgents.length}
          total={matchingAgents.length}
          onPage={setAgentPage}
        />
      </Stack>
    </Stack>
  );
}

function AgentSearch({
  query,
  onQuery,
  matching,
  total,
}: {
  query: string;
  onQuery: (next: string) => void;
  matching: number;
  total: number;
}) {
  return (
    <Stack direction="horizontal" align="center" gap="cozy">
      <Stack grow maxWidth="search-field">
        <Input
          compact
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Search agents…"
          aria-label="Search agents"
        />
      </Stack>
      <Text as="span" size="caption" color="subtle">
        {matching} of {total}
      </Text>
    </Stack>
  );
}

function Pager({
  page,
  pageCount,
  shown,
  total,
  onPage,
}: {
  page: number;
  pageCount: number;
  shown: number;
  total: number;
  onPage: (next: number) => void;
}) {
  if (pageCount <= 1) return null;
  return (
    <Stack direction="horizontal" align="center" justify="end" gap="snug">
      <Text as="span" size="caption" color="subtle">
        {page * AGENT_PAGE_SIZE + 1}–{page * AGENT_PAGE_SIZE + shown} of {total}
      </Text>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={page === 0}
        aria-label="Previous agents"
        onClick={() => onPage(page - 1)}
      >
        <Icon name="chevron-left" size="2xs" />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={page >= pageCount - 1}
        aria-label="More agents"
        onClick={() => onPage(page + 1)}
      >
        <Icon name="chevron-right" size="2xs" />
      </Button>
    </Stack>
  );
}

interface ChooseProps {
  routing: RoutingDraft;
  onRouting: (next: Partial<RoutingDraft>) => void;
  options: RoutingOptions;
}

function ChooseSettings({
  routing,
  onRouting,
  options,
  joinKeyExample = '{{ repository.full_name }}#{{ issue.number }}',
}: ChooseProps & { joinKeyExample?: string }) {
  const { productName } = useBrand();
  // With no decision model the mode cannot work, so nothing else is offered.
  if (noDecisionModels(options)) {
    return (
      <Callout variant="warning">
        <Stack gap="tight">
          <Text size="caption" weight="emphasis">
            No decision model is enabled for this workspace.
          </Text>
          <Text size="caption" color="muted">
            Let {productName} choose needs one. Ask your platform team to enable a decision model in
            the model catalog. Until then, route this trigger to one agent.
          </Text>
        </Stack>
      </Callout>
    );
  }
  const agents = options.candidates.status === 'ready' ? options.candidates.items : [];
  const candidates = agents.filter((a) => routing.candidateIds.includes(a.id));
  return (
    <Stack gap="relaxed">
      <CandidatePicker routing={routing} onRouting={onRouting} options={options} />
      <ModelPicker routing={routing} onRouting={onRouting} options={options} />

      <Stack gap="snug">
        <SectionLabel>Confidence</SectionLabel>
        <Text size="caption" color="subtle">
          How sure {productName} must be before it routes a delivery.
        </Text>
        <RadioGroup
          value={routing.confidence}
          onValueChange={(v) => onRouting({ confidence: v as TriggerRoutingConfidence })}
          aria-label="Confidence"
        >
          <Grid cols={3} gap="cozy">
            {confidenceOptions(productName).map((o) => (
              <RadioCard
                key={o.id}
                value={o.id}
                selected={routing.confidence === o.id}
                onSelect={() => onRouting({ confidence: o.id })}
                title={o.title}
                description={o.description}
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      <Stack gap="cozy">
        <SectionLabel>When {productName} is unsure</SectionLabel>
        <RadioGroup
          value={routing.unsure}
          onValueChange={(v) => onRouting({ unsure: v as UnsureAction })}
          aria-label={`When ${productName} is unsure`}
        >
          <Grid cols={3} gap="cozy">
            <RadioCard
              value="fallback"
              selected={routing.unsure === 'fallback'}
              onSelect={() => onRouting({ unsure: 'fallback' })}
              icon="user-check"
              title="Send to a fallback agent"
              description={`One of the candidates takes every delivery ${productName} is unsure about.`}
            />
            <RadioCard
              value="none"
              selected={routing.unsure === 'none'}
              onSelect={() => onRouting({ unsure: 'none' })}
              icon="x-circle"
              title="Don't start a run"
              description={`The delivery is recorded with ${productName}’s scores and nothing runs.`}
            />
            <RadioCard
              value="ask"
              selected={false}
              onSelect={() => undefined}
              icon="send"
              title="Ask in the conversation"
              description="Coming later."
              disabled
            />
          </Grid>
        </RadioGroup>
        {routing.unsure === 'fallback' && (
          <FormField
            label="Fallback agent"
            id="routing-fallback"
            hint={
              candidates.length === 0
                ? 'Pick candidate agents first.'
                : 'Limited to the candidate agents above.'
            }
          >
            <NativeSelect
              id="routing-fallback"
              value={routing.fallbackId}
              disabled={candidates.length === 0}
              onChange={(e) => onRouting({ fallbackId: e.target.value })}
            >
              <option value="">Choose an agent</option>
              {candidates.map((a) => (
                <option key={a.id} value={a.id}>
                  {agentLabel(a)}
                </option>
              ))}
            </NativeSelect>
          </FormField>
        )}
      </Stack>

      <Stack gap="cozy">
        <ToggleRow
          label="Join open runs"
          description={`${productName} may send a delivery to an open run of a candidate agent started by a trigger. Runs people started are never joined.`}
          checked={routing.joinOpenRuns}
          onChange={(next) => onRouting({ joinOpenRuns: next })}
        />
        {routing.joinOpenRuns && (
          <Card inset>
            <CardContent padded compact>
              <FormField
                label="Join key"
                id="routing-join-key"
                hint={`Optional. Template, same syntax as the prompt. A delivery whose key matches an open run of this trigger joins it without asking ${productName}.`}
              >
                <Input
                  id="routing-join-key"
                  mono
                  value={routing.joinKey}
                  onChange={(e) => onRouting({ joinKey: e.target.value })}
                  placeholder={joinKeyExample}
                />
              </FormField>
            </CardContent>
          </Card>
        )}
      </Stack>

      <Callout variant="info">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Icon name="file-text" size="sm" color="accent" label="" shrink />
          <Text size="caption" color="muted">
            The trigger keeps one prompt. Whichever agent {productName} picks receives the same
            prompt.
          </Text>
        </Stack>
      </Callout>
    </Stack>
  );
}

function CandidatePicker({ routing, onRouting, options }: ChooseProps) {
  const { productName } = useBrand();
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const list = options.candidates;
  const agents = list.status === 'ready' ? list.items : [];
  const q = query.trim().toLowerCase();
  const matching =
    q === ''
      ? agents
      : agents.filter(
          (a) => a.slug.toLowerCase().includes(q) || (a.name ?? '').toLowerCase().includes(q),
        );
  const pageCount = Math.max(1, Math.ceil(matching.length / AGENT_PAGE_SIZE));
  const current = Math.min(page, pageCount - 1);
  const visible = matching.slice(current * AGENT_PAGE_SIZE, (current + 1) * AGENT_PAGE_SIZE);

  const toggle = (id: string, on: boolean) => {
    const candidateIds = on
      ? [...routing.candidateIds, id]
      : routing.candidateIds.filter((c) => c !== id);
    onRouting({
      candidateIds,
      fallbackId: candidateIds.includes(routing.fallbackId) ? routing.fallbackId : '',
    });
  };

  return (
    <Stack gap="cozy">
      <Stack direction="horizontal" align="center" justify="between">
        <SectionLabel>Candidate agents</SectionLabel>
        <Text as="span" size="caption" color="subtle">
          {routing.candidateIds.length} selected
        </Text>
      </Stack>
      <Text size="caption" color="subtle">
        {productName} routes on each agent’s description, so a clear description routes better. Only
        agents this trigger may run can be picked.
      </Text>
      {list.status === 'loading' && (
        <Text size="caption" color="subtle">
          Loading agents…
        </Text>
      )}
      {list.status === 'error' && <Callout variant="error">{list.message}</Callout>}
      {list.status === 'ready' && agents.length === 0 && (
        <Callout variant="warning">
          No agents are available. Create an autonomous agent first.
        </Callout>
      )}
      {agents.length > AGENT_SEARCH_THRESHOLD && (
        <AgentSearch
          query={query}
          onQuery={(next) => {
            setQuery(next);
            setPage(0);
          }}
          matching={matching.length}
          total={agents.length}
        />
      )}
      <Stack gap="snug">
        {agents.length > 0 && visible.length === 0 && (
          <Text size="caption" color="subtle">
            No agents match this search.
          </Text>
        )}
        {visible.map((a) => (
          <CandidateCard
            key={a.id}
            agent={a}
            checked={routing.candidateIds.includes(a.id)}
            onToggle={(on) => toggle(a.id, on)}
          />
        ))}
      </Stack>
      <Pager
        page={current}
        pageCount={pageCount}
        shown={visible.length}
        total={matching.length}
        onPage={setPage}
      />
    </Stack>
  );
}

function CandidateCard({
  agent: a,
  checked,
  onToggle,
}: {
  agent: RoutingAgent;
  checked: boolean;
  onToggle: (on: boolean) => void;
}) {
  const { productName } = useBrand();
  const enabled = a.runnable;
  return (
    <Card
      interactive={enabled}
      subtle={!enabled}
      selected={checked}
      onClick={enabled ? () => onToggle(!checked) : undefined}
    >
      <CardContent padded compact>
        <Stack direction="horizontal" align="start" gap="cozy">
          <IconTile tone={checked ? 'accent' : 'muted'}>
            <Text as="span" size="caption" mono weight="heavy">
              {initial(a)}
            </Text>
          </IconTile>
          <Stack gap="tight" grow minWidth="none">
            <Stack direction="horizontal" align="center" gap="snug">
              <Text as="span" size="supporting" weight="heavy">
                {agentLabel(a)}
              </Text>
              <Text as="span" size="micro" mono color="subtle">
                {a.slug}
              </Text>
            </Stack>
            {a.description.trim() !== '' && (
              <Text size="caption" color="muted">
                {a.description}
              </Text>
            )}
            {!enabled && a.reason !== undefined && (
              <Text size="caption" color="subtle">
                Cannot be picked: {a.reason}.
              </Text>
            )}
            {enabled && needsDescription(a) && (
              <Stack direction="horizontal" align="center" gap="tight">
                <Icon name="triangle-alert" size="2xs" color="status-degraded" label="" />
                <Text as="span" size="caption" color="status-degraded">
                  Add a description so {productName} can route to this agent.
                </Text>
              </Stack>
            )}
          </Stack>
          {enabled && <Checkbox checked={checked} onChange={onToggle} aria-label={agentLabel(a)} />}
        </Stack>
      </CardContent>
    </Card>
  );
}

/** Columns for the decision model table, the paged radio-per-row list the environment wizard uses for images. */
const MODEL_COLUMNS: DataTableColumn<DecisionModel>[] = [
  {
    id: 'select',
    header: '',
    gridWidth: '24px',
    locked: true,
    cell: (m) => <RadioGroupItem value={m.id} aria-label={m.name} />,
  },
  {
    id: 'name',
    header: 'Name',
    gridWidth: 'minmax(0, 1fr)',
    wrap: true,
    locked: true,
    cell: (m) => (
      <Text as="span" size="supporting" weight="heavy">
        {m.name}
      </Text>
    ),
  },
  {
    id: 'id',
    header: 'Model',
    gridWidth: 'minmax(0, 1.4fr)',
    wrap: true,
    locked: true,
    cell: (m) => (
      <Text as="span" size="micro" mono color="subtle">
        {m.id}
      </Text>
    ),
  },
];

function ModelPicker({ routing, onRouting, options }: ChooseProps) {
  const { productName } = useBrand();
  const list = options.models;
  // The selected model always matches, so filtering never hides the current choice.
  const searchModels = (m: DecisionModel, query: string) =>
    m.id === routing.modelId ||
    [m.name, m.id].some((v) => v.toLowerCase().includes(query.toLowerCase()));
  return (
    <Stack gap="cozy">
      <SectionLabel>Decision model</SectionLabel>
      <Text size="caption" color="subtle">
        The model {productName} asks which agent fits. Only decision models from your model catalog
        are listed.
      </Text>
      {list.status === 'loading' && (
        <Text size="caption" color="subtle">
          Loading decision models…
        </Text>
      )}
      {list.status === 'error' && <Callout variant="error">{list.message}</Callout>}
      {list.status === 'ready' && (
        <RadioGroup
          value={routing.modelId}
          onValueChange={(v) => onRouting({ modelId: v })}
          aria-label="Decision model"
        >
          <DataTable
            data={list.items}
            columns={MODEL_COLUMNS}
            getRowKey={(m) => m.id}
            onRowClick={(m) => onRouting({ modelId: m.id })}
            rowLabel={(m) => `Use ${m.name}`}
            emptyMessage="No decision models are enabled."
            filteredEmptyMessage="No decision models match. Only models with the decision capability are listed."
            searchFn={searchModels}
            searchPlaceholder="Search decision models"
            noun="decision models"
            pagination={{ pageSizeOptions: [5, 10, 20], defaultPageSize: 5 }}
          />
        </RadioGroup>
      )}
    </Stack>
  );
}
