'use client';

/** @tier 2 — Orbit-domain-aware: governed model selection (family groups, pricing, threshold search). Used by: agents builder, agents configuration, model-gateway routes (planned). */

import { useMemo, useState } from 'react';
import {
  Card,
  CardContent,
  Icon,
  Input,
  RadioGroup,
  RadioGroupItem,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import type { ModelObject } from '@orbit/shared/api';

function formatPrice(pricing?: {
  effective_input_cost_per_1m?: number;
  effective_output_cost_per_1m?: number;
}) {
  if (!pricing) return null;
  const inp = pricing.effective_input_cost_per_1m;
  const out = pricing.effective_output_cost_per_1m;
  if (inp == null || out == null) return null;
  const f = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/0$/, ''));
  return `$${f(inp)} / $${f(out)} · Mtok`;
}

// Below this many models the filter box is noise; above it, essential.
const MODEL_SEARCH_THRESHOLD = 8;

export function ModelPicker({
  models,
  value,
  onChange,
}: {
  models: ModelObject[];
  value: string;
  onChange: (id: string) => void;
}) {
  const [modelSearch, setModelSearch] = useState('');

  // Filtered and grouped by family, so 30+ models stay navigable.
  const modelFamilies = useMemo(() => {
    const q = modelSearch.trim().toLowerCase();
    const filtered = q
      ? models.filter((m) =>
          [m.display_name, m.id, m.owned_by, m.family].some((s) =>
            (s ?? '').toLowerCase().includes(q),
          ),
        )
      : models;
    const byFamily = new Map<string, typeof filtered>();
    for (const m of filtered) {
      const family = m.family || 'other';
      const list = byFamily.get(family);
      if (list) list.push(m);
      else byFamily.set(family, [m]);
    }
    return [...byFamily.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [models, modelSearch]);

  return (
    <Stack gap="cozy">
      {models.length > MODEL_SEARCH_THRESHOLD && (
        <Input
          value={modelSearch}
          onChange={(e) => setModelSearch(e.target.value)}
          placeholder="Filter models by name, id, or provider…"
          aria-label="Filter models"
        />
      )}

      <RadioGroup value={value} onValueChange={onChange} aria-label="Model">
        <Stack gap="cozy">
          {modelFamilies.map(([family, familyModels]) => (
            <Stack key={family} gap="snug">
              <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
                {family}
              </Text>
              <ModelRows models={familyModels} selectedId={value} onSelect={onChange} />
            </Stack>
          ))}
          {modelFamilies.length === 0 && models.length > 0 && (
            <Text size="caption" color="subtle">
              No models match “{modelSearch}”.
            </Text>
          )}
        </Stack>
      </RadioGroup>
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
