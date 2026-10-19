'use client';

/** @tier 3 — triggers-scoped. Proposed Target step: route every delivery to one
 * agent, or let kora pick the agent and the run for each delivery. Storybook only. */

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
import type { ModelObject } from '@runkora/shared/api';
import { StepTarget } from './step-target';
import type { StepComponentProps, StepValidation, TriggerWizardAgent } from './types';

/** An agent as the routing picker sees it: the description is what kora routes on. */
export interface RoutingAgent extends TriggerWizardAgent {
  description?: string;
  /** False when the trigger's principal holds no run grant for this agent. */
  canRun: boolean;
}

export type RoutingMode = 'one' | 'choose';
export type ConfidencePreset = 'strict' | 'balanced' | 'loose';
export type UnsureAction = 'fallback' | 'none';

export interface RoutingDraft {
  mode: RoutingMode;
  candidateIds: string[];
  modelId: string;
  confidence: ConfidencePreset;
  unsure: UnsureAction;
  fallbackId: string;
  joinOpenRuns: boolean;
}

export const DEFAULT_ROUTING: RoutingDraft = {
  mode: 'one',
  candidateIds: [],
  modelId: '',
  confidence: 'balanced',
  unsure: 'fallback',
  fallbackId: '',
  joinOpenRuns: true,
};

/** A model the picker can offer: display name plus the family/name id. */
export interface DecisionModel {
  id: string;
  name: string;
}

export type DecisionCatalog =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; models: DecisionModel[] };

/** The tenant catalog narrowed to models that can answer a routing question. */
export function decisionModelsOf(models: ModelObject[]): DecisionModel[] {
  return models
    .filter((m) => m.status === 'available' && m.capabilities.includes('decision'))
    .map((m) => ({ id: m.id, name: m.display_name || m.id }));
}

/** Below this many characters a description is too thin to route on. */
const MIN_DESCRIPTION = 40;

export function needsDescription(agent: RoutingAgent): boolean {
  return (agent.description ?? '').trim().length < MIN_DESCRIPTION;
}

function selectable(agent: RoutingAgent): boolean {
  return agent.canRun && agent.identityMode !== 'delegated';
}

const AGENT_SEARCH_THRESHOLD = 6;
const AGENT_PAGE_SIZE = 5;

const CONFIDENCE_OPTIONS: { id: ConfidencePreset; title: string; description: string }[] = [
  {
    id: 'strict',
    title: 'Strict',
    description: 'Only routes when kora is sure. More deliveries go to the fallback.',
  },
  {
    id: 'balanced',
    title: 'Balanced',
    description: 'Routes when kora is fairly sure. Suits most triggers.',
  },
  {
    id: 'loose',
    title: 'Loose',
    description: 'Routes on kora’s best guess. Fewer fallbacks, more wrong picks.',
  },
];

export function validateRouting(draft: RoutingDraft, catalog: DecisionCatalog): StepValidation {
  if (draft.mode === 'one') return { valid: true };
  if (draft.candidateIds.length < 2)
    return { valid: false, hint: 'Pick at least two agents for kora to choose between.' };
  if (catalog.status === 'ready' && catalog.models.length === 0)
    // The step shows the reason inline, so no banner repeats it.
    return { valid: false };
  if (draft.modelId === '') return { valid: false, hint: 'Choose a decision model.' };
  if (draft.unsure === 'fallback' && !draft.candidateIds.includes(draft.fallbackId))
    return { valid: false, hint: 'Choose the fallback agent.' };
  return { valid: true };
}

export interface StepTargetProposedProps extends StepComponentProps {
  routing: RoutingDraft;
  onRouting: (next: Partial<RoutingDraft>) => void;
  agents: RoutingAgent[];
  catalog: DecisionCatalog;
}

export function StepTargetProposed(props: StepTargetProposedProps) {
  const { routing, onRouting } = props;
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
            title="Let kora choose"
            description="kora picks the agent for each delivery, and whether it starts a run or joins an open one."
          />
        </Grid>
      </RadioGroup>
      {routing.mode === 'one' ? <StepTarget {...props} /> : <ChooseSettings {...props} />}
    </Stack>
  );
}

function ChooseSettings({ routing, onRouting, agents, catalog }: StepTargetProposedProps) {
  const candidates = agents.filter((a) => routing.candidateIds.includes(a.id));
  // With no decision model the mode cannot work, so nothing else is offered.
  if (catalog.status === 'ready' && catalog.models.length === 0) {
    return (
      <Callout variant="warning">
        <Stack gap="tight">
          <Text size="caption" weight="emphasis">
            No decision model is enabled for this workspace.
          </Text>
          <Text size="caption" color="muted">
            Let kora choose needs one. Ask your platform team to enable a decision model in the
            model catalog. Until then, route this trigger to one agent.
          </Text>
        </Stack>
      </Callout>
    );
  }
  return (
    <Stack gap="relaxed">
      <CandidatePicker routing={routing} onRouting={onRouting} agents={agents} />
      <ModelPicker routing={routing} onRouting={onRouting} catalog={catalog} />

      <Stack gap="snug">
        <SectionLabel>Confidence</SectionLabel>
        <Text size="caption" color="subtle">
          How sure kora must be before it routes a delivery.
        </Text>
        <RadioGroup
          value={routing.confidence}
          onValueChange={(v) => onRouting({ confidence: v as ConfidencePreset })}
          aria-label="Confidence"
        >
          <Grid cols={3} gap="cozy">
            {CONFIDENCE_OPTIONS.map((o) => (
              <RadioCard
                key={o.id}
                value={o.id}
                selected={routing.confidence === o.id}
                onSelect={() => onRouting({ confidence: o.id })}
                title={o.id === 'balanced' ? `${o.title} (default)` : o.title}
                description={o.description}
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      <Stack gap="cozy">
        <SectionLabel>When kora is unsure</SectionLabel>
        <RadioGroup
          value={routing.unsure}
          onValueChange={(v) => onRouting({ unsure: v as UnsureAction })}
          aria-label="When kora is unsure"
        >
          <Grid cols={3} gap="cozy">
            <RadioCard
              value="fallback"
              selected={routing.unsure === 'fallback'}
              onSelect={() => onRouting({ unsure: 'fallback' })}
              icon="user-check"
              title="Send to a fallback agent"
              description="One of the candidates takes every delivery kora is unsure about."
            />
            <RadioCard
              value="none"
              selected={routing.unsure === 'none'}
              onSelect={() => onRouting({ unsure: 'none' })}
              icon="x-circle"
              title="Don't start a run"
              description="The delivery is recorded with kora’s scores and nothing runs."
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
                  {a.name?.trim() || a.slug}
                </option>
              ))}
            </NativeSelect>
          </FormField>
        )}
      </Stack>

      <ToggleRow
        label="Join open runs"
        description="Deliveries with the same coalesce key always join that run. Otherwise kora may send the delivery to an open run started by a trigger. Runs people started are never joined."
        checked={routing.joinOpenRuns}
        onChange={(next) => onRouting({ joinOpenRuns: next })}
      />

      <Callout variant="info">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Icon name="file-text" size="sm" color="accent" label="" shrink />
          <Text size="caption" color="muted">
            The trigger keeps one prompt. Whichever agent kora picks receives the same prompt.
          </Text>
        </Stack>
      </Callout>
    </Stack>
  );
}

function initial(agent: TriggerWizardAgent): string {
  const source = agent.name?.trim() || agent.slug;
  return (source.charAt(0) || '?').toUpperCase();
}

function CandidatePicker({
  routing,
  onRouting,
  agents,
}: Pick<StepTargetProposedProps, 'routing' | 'onRouting' | 'agents'>) {
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
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
        kora routes on each agent’s description, so a clear description routes better. Only agents
        this trigger may run can be picked.
      </Text>
      {agents.length > AGENT_SEARCH_THRESHOLD && (
        <Stack direction="horizontal" align="center" gap="cozy">
          <Stack grow maxWidth="search-field">
            <Input
              compact
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(0);
              }}
              placeholder="Search agents…"
              aria-label="Search agents"
            />
          </Stack>
          <Text as="span" size="caption" color="subtle">
            {matching.length} of {agents.length}
          </Text>
        </Stack>
      )}
      <Stack gap="snug">
        {visible.length === 0 && (
          <Text size="caption" color="subtle">
            No agents match this search.
          </Text>
        )}
        {visible.map((a) => {
          const enabled = selectable(a);
          const checked = routing.candidateIds.includes(a.id);
          const reason =
            a.identityMode === 'delegated'
              ? 'delegated · started by people'
              : !a.canRun
                ? 'no run permission'
                : null;
          return (
            <Card
              key={a.id}
              interactive={enabled}
              subtle={!enabled}
              selected={checked}
              onClick={enabled ? () => toggle(a.id, !checked) : undefined}
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
                        {a.name?.trim() || a.slug}
                      </Text>
                      <Text as="span" size="micro" mono color="subtle">
                        {a.slug}
                      </Text>
                    </Stack>
                    {(a.description ?? '').trim() !== '' && (
                      <Text size="caption" color="muted">
                        {a.description}
                      </Text>
                    )}
                    {enabled && needsDescription(a) && (
                      <Stack direction="horizontal" align="center" gap="tight">
                        <Icon name="triangle-alert" size="2xs" color="status-degraded" label="" />
                        <Text as="span" size="caption" color="status-degraded">
                          Add a description so kora can route to this agent.
                        </Text>
                      </Stack>
                    )}
                  </Stack>
                  {reason !== null ? (
                    <Stack shrink={false}>
                      <Text as="span" size="micro" color="subtle">
                        {reason}
                      </Text>
                    </Stack>
                  ) : (
                    <Checkbox
                      checked={checked}
                      onChange={(on) => toggle(a.id, on)}
                      aria-label={a.name?.trim() || a.slug}
                    />
                  )}
                </Stack>
              </CardContent>
            </Card>
          );
        })}
      </Stack>
      {pageCount > 1 && (
        <Stack direction="horizontal" align="center" justify="end" gap="snug">
          <Text as="span" size="caption" color="subtle">
            {current * AGENT_PAGE_SIZE + 1}–{current * AGENT_PAGE_SIZE + visible.length} of{' '}
            {matching.length}
          </Text>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={current === 0}
            aria-label="Previous agents"
            onClick={() => setPage(current - 1)}
          >
            <Icon name="chevron-left" size="2xs" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={current >= pageCount - 1}
            aria-label="More agents"
            onClick={() => setPage(current + 1)}
          >
            <Icon name="chevron-right" size="2xs" />
          </Button>
        </Stack>
      )}
    </Stack>
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

function ModelPicker({
  routing,
  onRouting,
  catalog,
}: Pick<StepTargetProposedProps, 'routing' | 'onRouting' | 'catalog'>) {
  // The selected model always matches, so filtering never hides the current choice.
  const searchModels = (m: DecisionModel, query: string) =>
    m.id === routing.modelId ||
    [m.name, m.id].some((v) => v.toLowerCase().includes(query.toLowerCase()));
  return (
    <Stack gap="cozy">
      <SectionLabel>Decision model</SectionLabel>
      <Text size="caption" color="subtle">
        The model kora asks which agent fits. Only decision models from your model catalog are
        listed.
      </Text>
      {catalog.status === 'loading' && (
        <Text size="caption" color="subtle">
          Loading decision models…
        </Text>
      )}
      {catalog.status === 'error' && <Callout variant="error">{catalog.message}</Callout>}
      {catalog.status === 'ready' && (
        <RadioGroup
          value={routing.modelId}
          onValueChange={(v) => onRouting({ modelId: v })}
          aria-label="Decision model"
        >
          <DataTable
            data={catalog.models}
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
