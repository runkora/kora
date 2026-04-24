'use client';

import { useState, useRef } from 'react';
import {
  Alert,
  Button,
  DragCard,
  Icon,
  Field,
  Grid,
  Input,
  Label,
  NativeSelect,
  Separator,
  Stack,
  StepDot,
  Text,
  Toggle,
} from '@orbit/terra';
import type { VirtualModel, ProviderModel, Provider } from '../_types/catalog';
import { PROVIDER_LABELS } from '../_utils/catalog-constants';

interface RoutesTabProps {
  model: VirtualModel;
  onChange: (m: VirtualModel) => void;
  providerModels: ProviderModel[];
}

export function RoutesTab({ model, onChange, providerModels }: RoutesTabProps) {
  const availableProviders = Array.from(
    new Set(providerModels.map((m) => m.provider)),
  ) as Provider[];
  const providersWithModels =
    availableProviders.length > 0
      ? availableProviders
      : (['bedrock', 'vertexai', 'azure'] as Provider[]);

  function modelsForProvider(provider: Provider): ProviderModel[] {
    return providerModels.filter((m) => m.provider === provider);
  }

  const dragIdx = useRef<number | null>(null);
  const [draggingIdx, setDraggingIdx] = useState<number | null>(null);
  const [overIdx, setOverIdx] = useState<number | null>(null);

  function handleDragStart(idx: number) {
    dragIdx.current = idx;
    setDraggingIdx(idx);
  }

  function handleDragOver(e: React.DragEvent, idx: number) {
    e.preventDefault();
    setOverIdx(idx);
  }

  function handleDragLeave() {
    setOverIdx(null);
  }

  function handleDrop(targetIdx: number) {
    const from = dragIdx.current;
    dragIdx.current = null;
    setDraggingIdx(null);
    setOverIdx(null);
    if (from === null || from === targetIdx) return;
    const routes = [...model.routes];
    const [moved] = routes.splice(from, 1);
    routes.splice(targetIdx, 0, moved);
    onChange({ ...model, routes: routes.map((r, i) => ({ ...r, priority: i + 1 })) });
  }

  function handleDragEnd() {
    dragIdx.current = null;
    setDraggingIdx(null);
    setOverIdx(null);
  }

  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <Text size="supporting" weight="emphasis">
          Routing Strategy
        </Text>
        <Grid cols={3} gap="snug">
          {[
            {
              value: 'priority' as const,
              label: 'Priority / Failover',
              desc: 'Try routes in order. Move to next on failure.',
              iconName: 'arrow-up-down' as const,
            },
            {
              value: 'weighted' as const,
              label: 'Weighted Split',
              desc: 'Distribute traffic by percentage across routes.',
              iconName: 'network' as const,
            },
            {
              value: 'lowest_cost' as const,
              label: 'Lowest Cost',
              desc: 'Always route to the cheapest available provider.',
              iconName: 'dollar-sign' as const,
            },
          ].map((s) => (
            <Button
              key={s.value}
              asChild
              variant={model.strategy === s.value ? 'secondary' : 'outline'}
              onClick={() => onChange({ ...model, strategy: s.value })}
            >
              <Stack gap="tight" align="start" paddingBlock="snug" paddingInline="snug">
                <Icon name={s.iconName} color={model.strategy === s.value ? 'accent' : 'muted'} />
                <Text as="span" size="caption" weight="emphasis">
                  {s.label}
                </Text>
                <Text as="span" size="caption" color="muted">
                  {s.desc}
                </Text>
              </Stack>
            </Button>
          ))}
        </Grid>
      </Stack>

      <Separator />

      <Stack gap="snug">
        <Stack direction="horizontal" align="center" justify="between" gap="snug">
          <Text size="supporting" weight="emphasis">
            Routes
          </Text>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              onChange({
                ...model,
                routes: [
                  ...model.routes,
                  {
                    id: crypto.randomUUID(),
                    provider: 'bedrock',
                    modelId: '',
                    priority: model.routes.length + 1,
                    weight: 0,
                    inputCostPer1M: 0,
                    outputCostPer1M: 0,
                    enabled: true,
                  },
                ],
              });
            }}
          >
            <Icon name="plus" size="2xs" />
            Add Route
          </Button>
        </Stack>

        {model.strategy === 'weighted' && (
          <Alert variant="warning">
            <Icon name="alert-circle" size="2xs" color="status-degraded" />
            <Text size="caption" color="muted">
              Weights must sum to 100%. Current total:{' '}
              <Text asChild size="caption" weight="emphasis" color="foreground">
                <output>{model.routes.reduce((s, r) => s + r.weight, 0)}%</output>
              </Text>
            </Text>
          </Alert>
        )}

        <Stack gap="snug">
          {model.routes.map((route, idx) => (
            <DragCard
              key={route.id || idx}
              draggable
              dragging={draggingIdx === idx}
              dropTarget={overIdx === idx && draggingIdx !== null}
              onDragStart={() => handleDragStart(idx)}
              onDragOver={(e) => handleDragOver(e, idx)}
              onDragLeave={handleDragLeave}
              onDrop={() => handleDrop(idx)}
              onDragEnd={handleDragEnd}
            >
              {/* Left column: drag handle + priority number */}
              <Stack direction="vertical" align="center" gap="tight" shrink paddingTop="snug">
                <Icon name="grip-vertical" color="muted" cursorGrab />
                {model.strategy === 'priority' && (
                  <StepDot variant="surface" size="sm">
                    {idx + 1}
                  </StepDot>
                )}
              </Stack>

              {/* Center column: all fields stacked */}
              <Stack direction="vertical" grow gap="snug" minWidth="none">
                <NativeSelect
                  value={route.provider}
                  onChange={(e) => {
                    const updated = model.routes.map((r, i) =>
                      i === idx ? { ...r, provider: e.target.value as Provider, modelId: '' } : r,
                    );
                    onChange({ ...model, routes: updated });
                  }}
                >
                  {providersWithModels.map((p) => (
                    <option key={p} value={p}>
                      {PROVIDER_LABELS[p] ?? p}
                    </option>
                  ))}
                </NativeSelect>

                {modelsForProvider(route.provider).length > 0 ? (
                  <NativeSelect
                    value={route.modelId}
                    onChange={(e) => {
                      const updated = model.routes.map((r, i) =>
                        i === idx ? { ...r, modelId: e.target.value } : r,
                      );
                      onChange({ ...model, routes: updated });
                    }}
                    mono
                  >
                    <option value="">— select a model —</option>
                    {Array.from(new Set(modelsForProvider(route.provider).map((m) => m.family)))
                      .sort()
                      .map((family) => (
                        <optgroup key={family} label={family}>
                          {modelsForProvider(route.provider)
                            .filter((m) => m.family === family)
                            .map((m) => (
                              <option key={m.modelId} value={m.modelId}>
                                {m.displayName || m.modelId}
                              </option>
                            ))}
                        </optgroup>
                      ))}
                  </NativeSelect>
                ) : (
                  <Input
                    mono
                    value={route.modelId}
                    onChange={(e) => {
                      const updated = model.routes.map((r, i) =>
                        i === idx ? { ...r, modelId: e.target.value } : r,
                      );
                      onChange({ ...model, routes: updated });
                    }}
                    placeholder="Provider model ID"
                  />
                )}

                <Grid cols={2} gap="snug">
                  <Field>
                    <Label>
                      <Text size="caption" color="muted">
                        Input /1M
                      </Text>
                    </Label>
                    <Input
                      type="number"
                      value={route.inputCostPer1M}
                      step="0.01"
                      min="0"
                      onChange={(e) => {
                        const updated = model.routes.map((r, i) =>
                          i === idx ? { ...r, inputCostPer1M: parseFloat(e.target.value) || 0 } : r,
                        );
                        onChange({ ...model, routes: updated });
                      }}
                    />
                  </Field>
                  <Field>
                    <Label>
                      <Text size="caption" color="muted">
                        Output /1M
                      </Text>
                    </Label>
                    <Input
                      type="number"
                      value={route.outputCostPer1M}
                      step="0.01"
                      min="0"
                      onChange={(e) => {
                        const updated = model.routes.map((r, i) =>
                          i === idx
                            ? { ...r, outputCostPer1M: parseFloat(e.target.value) || 0 }
                            : r,
                        );
                        onChange({ ...model, routes: updated });
                      }}
                    />
                  </Field>
                </Grid>
              </Stack>

              {/* Right column: weight (weighted only) + toggle + delete */}
              <Stack direction="vertical" align="end" gap="snug" shrink>
                {model.strategy === 'weighted' && (
                  <Stack direction="horizontal" align="center" gap="tight">
                    <Stack asChild maxWidth="form-field-sm">
                      <Input
                        type="number"
                        value={route.weight}
                        min={0}
                        max={100}
                        onChange={(e) => {
                          const updated = model.routes.map((r, i) =>
                            i === idx ? { ...r, weight: parseInt(e.target.value) || 0 } : r,
                          );
                          onChange({ ...model, routes: updated });
                        }}
                      />
                    </Stack>
                    <Text size="caption" color="muted">
                      %
                    </Text>
                  </Stack>
                )}
                <Stack direction="horizontal" align="center" gap="tight">
                  <Toggle
                    checked={route.enabled}
                    onChange={(v) => {
                      const updated = model.routes.map((r, i) =>
                        i === idx ? { ...r, enabled: v } : r,
                      );
                      onChange({ ...model, routes: updated });
                    }}
                  />
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() =>
                      onChange({ ...model, routes: model.routes.filter((_, i) => i !== idx) })
                    }
                    aria-label="Remove route"
                  >
                    <Icon name="trash" size="2xs" />
                  </Button>
                </Stack>
              </Stack>
            </DragCard>
          ))}
        </Stack>

        {model.strategy === 'priority' && (
          <Text size="caption" color="muted">
            Route 1 is primary. On failure the gateway tries subsequent routes in order.
          </Text>
        )}
        {model.strategy === 'lowest_cost' && (
          <Text size="caption" color="muted">
            At request time the gateway selects the cheapest available route. Failover still applies
            if the selected route errors.
          </Text>
        )}
      </Stack>
    </Stack>
  );
}
