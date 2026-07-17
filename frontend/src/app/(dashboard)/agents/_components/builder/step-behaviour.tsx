'use client';

/** @tier 3 — agents-scoped. Builder step 2: system instructions, framework, model. */

import { useMemo, useState } from 'react';
import {
  Alert,
  Card,
  CardContent,
  FormField,
  Grid,
  Icon,
  Input,
  RadioGroup,
  RadioGroupItem,
  Separator,
  Stack,
  StatusDot,
  Text,
  Textarea,
} from '@orbit/terra';
import type { AgentFramework, ModelObject } from '@orbit/shared/api';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { StepHeader } from './step-header';

const FRAMEWORKS: { id: AgentFramework; name: string; desc: string }[] = [
  { id: 'claude-code', name: 'Claude Code', desc: "Anthropic's agentic coding harness." },
  { id: 'codex', name: 'Codex', desc: "OpenAI's coding agent runtime." },
  { id: 'opencode', name: 'Open Code', desc: 'Open-source, model-agnostic agent.' },
];

function formatPrice(pricing?: {
  effective_input_cost_per_1m: number;
  effective_output_cost_per_1m: number;
}) {
  if (!pricing) return null;
  const f = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/0$/, ''));
  return `$${f(pricing.effective_input_cost_per_1m)} / $${f(pricing.effective_output_cost_per_1m)} · Mtok`;
}

// Below this many models the filter box is noise; above it, essential.
const MODEL_SEARCH_THRESHOLD = 8;

export function StepBehaviour() {
  const { fields, setField, catalogues } = useAgentBuilderContext();
  const [modelSearch, setModelSearch] = useState('');

  // Filtered and grouped by family, so 30+ models stay navigable.
  const modelFamilies = useMemo(() => {
    const q = modelSearch.trim().toLowerCase();
    const filtered = q
      ? catalogues.models.filter((m) =>
          [m.display_name, m.id, m.owned_by, m.family].some((s) =>
            (s ?? '').toLowerCase().includes(q),
          ),
        )
      : catalogues.models;
    const byFamily = new Map<string, typeof filtered>();
    for (const m of filtered) {
      const family = m.family || 'other';
      const list = byFamily.get(family);
      if (list) list.push(m);
      else byFamily.set(family, [m]);
    }
    return [...byFamily.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [catalogues.models, modelSearch]);

  return (
    <Stack gap="relaxed">
      <StepHeader
        icon="bot"
        title="Behaviour"
        description="Describe how the agent should behave, then choose the model and framework it runs on."
      />

      <FormField label="System instructions" required>
        <Textarea
          value={fields.systemPrompt}
          onChange={(e) => setField('systemPrompt', e.target.value)}
          rows={5}
          placeholder="You are a deployment assistant for the platform team. You help engineers roll out services safely. You always run policy checks before applying changes and summarise the blast radius first."
        />
      </FormField>

      <Separator span="full" />

      {/* ── Framework ──────────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <Text as="span" size="caption" weight="heavy" color="muted">
          Framework
        </Text>
        <RadioGroup
          value={fields.framework}
          onValueChange={(v) => setField('framework', v as AgentFramework)}
          aria-label="Framework"
        >
          <Grid cols={3} gap="cozy">
            {FRAMEWORKS.map((f) => {
              const selected = fields.framework === f.id;
              return (
                <Card
                  key={f.id}
                  interactive
                  selected={selected}
                  onClick={() => setField('framework', f.id)}
                >
                  <CardContent padded compact>
                    <Stack gap="snug">
                      <Stack direction="horizontal" align="center">
                        <Icon
                          name="square-terminal"
                          size="sm"
                          color={selected ? 'accent' : 'muted'}
                          container
                          containerVariant={selected ? 'accent' : 'surface-subtle'}
                          label=""
                        />
                        <Stack grow />
                        <RadioGroupItem value={f.id} aria-label={f.name} />
                      </Stack>
                      <Stack gap="tight">
                        <Text as="span" size="supporting" weight="heavy">
                          {f.name}
                        </Text>
                        <Text size="micro" color="muted">
                          {f.desc}
                        </Text>
                      </Stack>
                    </Stack>
                  </CardContent>
                </Card>
              );
            })}
          </Grid>
        </RadioGroup>
      </Stack>

      {/* ── Model ──────────────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <Stack direction="horizontal" align="center">
          <Text as="span" size="caption" weight="heavy" color="muted">
            Model
          </Text>
          <Stack grow />
          <Text as="span" size="micro" color="subtle">
            {catalogues.models.length} enabled by your platform
          </Text>
        </Stack>

        {catalogues.status.models === 'error' && (
          <Alert variant="error">Models failed to load: {catalogues.errors.models}</Alert>
        )}

        {catalogues.models.length > MODEL_SEARCH_THRESHOLD && (
          <Input
            value={modelSearch}
            onChange={(e) => setModelSearch(e.target.value)}
            placeholder="Filter models by name, id, or provider…"
            aria-label="Filter models"
          />
        )}

        <RadioGroup
          value={fields.modelId}
          onValueChange={(v) => setField('modelId', v)}
          aria-label="Model"
        >
          <Stack gap="cozy">
            {modelFamilies.map(([family, models]) => (
              <Stack key={family} gap="snug">
                <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
                  {family}
                </Text>
                <ModelRows
                  models={models}
                  selectedId={fields.modelId}
                  onSelect={(id) => setField('modelId', id)}
                />
              </Stack>
            ))}
            {modelFamilies.length === 0 && catalogues.status.models === 'success' && (
              <Text size="caption" color="subtle">
                No models match “{modelSearch}”.
              </Text>
            )}
          </Stack>
        </RadioGroup>
      </Stack>
    </Stack>
  );
}

function ModelRows({
  models,
  selectedId,
  onSelect,
}: {
  models: ModelObject[];
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  return (
    <Stack gap="snug">
      {models.map((m) => {
        const selected = selectedId === m.id;
        const price = formatPrice(m.pricing);
        return (
          <Card key={m.id} interactive selected={selected} onClick={() => onSelect(m.id)}>
            <CardContent padded compact>
              <Stack direction="horizontal" align="center" gap="cozy">
                <Icon
                  name="cpu"
                  size="sm"
                  color="muted"
                  container
                  containerVariant="surface-subtle"
                  label=""
                />
                <Stack grow gap="tight">
                  <Stack direction="horizontal" align="center" gap="snug">
                    <Text as="span" size="supporting" weight="heavy">
                      {m.display_name || m.id}
                    </Text>
                    <StatusDot size="sm" status={m.family === 'anthropic' ? 'accent' : 'idle'} />
                    <Text as="span" size="micro" color="subtle">
                      {m.owned_by}
                    </Text>
                  </Stack>
                  <Text as="span" size="micro" mono color="subtle">
                    {m.id}
                  </Text>
                </Stack>
                {price && (
                  <Text as="span" size="micro" mono color="subtle">
                    {price}
                  </Text>
                )}
                <RadioGroupItem value={m.id} aria-label={m.display_name || m.id} />
              </Stack>
            </CardContent>
          </Card>
        );
      })}
    </Stack>
  );
}
