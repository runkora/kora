'use client';

import { useState, useRef } from 'react';
import {
  AlertCircle,
  ArrowUpDown,
  DollarSign,
  GripVertical,
  Network,
  Plus,
  Trash2,
} from 'lucide-react';
import {
  Button,
  Field,
  Grid,
  Input,
  Label,
  NativeSelect,
  Separator,
  Stack,
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
              icon: <ArrowUpDown className="size-4" />,
            },
            {
              value: 'weighted' as const,
              label: 'Weighted Split',
              desc: 'Distribute traffic by percentage across routes.',
              icon: <Network className="size-4" />,
            },
            {
              value: 'lowest_cost' as const,
              label: 'Lowest Cost',
              desc: 'Always route to the cheapest available provider.',
              icon: <DollarSign className="size-4" />,
            },
          ].map((s) => (
            <button
              key={s.value}
              type="button"
              onClick={() => onChange({ ...model, strategy: s.value })}
              className={[
                'rounded-md border p-3 text-left transition-colors',
                model.strategy === s.value
                  ? 'border-accent bg-accent/10'
                  : 'border-border bg-surface-elevated hover:border-border/80',
              ].join(' ')}
            >
              <Stack gap="tight">
                <span className={model.strategy === s.value ? 'text-accent' : 'text-muted'}>
                  {s.icon}
                </span>
                <Text size="caption" weight="emphasis">
                  {s.label}
                </Text>
                <Text size="caption" color="muted">
                  {s.desc}
                </Text>
              </Stack>
            </button>
          ))}
        </Grid>
      </Stack>

      <Separator />

      <Stack gap="snug">
        <Stack direction="horizontal" align="center" gap="snug">
          <span className="flex-1">
            <Text size="supporting" weight="emphasis">
              Routes
            </Text>
          </span>
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
            <Plus className="mr-1.5 size-3.5" />
            Add Route
          </Button>
        </Stack>

        {model.strategy === 'weighted' && (
          <section className="flex items-center gap-2 rounded-md border border-[#a855f7]/30 bg-[#a855f7]/10 px-3 py-2">
            <AlertCircle className="size-3.5 shrink-0 text-[#a855f7]" />
            <Text size="caption" color="muted">
              Weights must sum to 100%. Current total:{' '}
              <span className="font-medium text-foreground">
                {model.routes.reduce((s, r) => s + r.weight, 0)}%
              </span>
            </Text>
          </section>
        )}

        <Stack gap="snug">
          {model.routes.map((route, idx) => (
            <section
              key={route.id || idx}
              draggable
              onDragStart={() => handleDragStart(idx)}
              onDragOver={(e) => handleDragOver(e, idx)}
              onDragLeave={handleDragLeave}
              onDrop={() => handleDrop(idx)}
              onDragEnd={handleDragEnd}
              className={[
                'flex gap-3 rounded-md border p-3 transition-all duration-150',
                draggingIdx === idx
                  ? 'opacity-40 scale-[0.98] border-border bg-surface-elevated'
                  : overIdx === idx && draggingIdx !== null
                    ? 'border-accent bg-accent/5 shadow-sm'
                    : 'border-border bg-surface-elevated',
              ].join(' ')}
            >
              {/* Left column: drag handle + priority number */}
              <span className="flex shrink-0 flex-col items-center gap-1 pt-2">
                <GripVertical className="size-4 cursor-grab text-muted active:cursor-grabbing" />
                {model.strategy === 'priority' && (
                  <span className="flex size-4 items-center justify-center rounded-full bg-surface text-[10px] font-bold text-muted">
                    {idx + 1}
                  </span>
                )}
              </span>

              {/* Center column: all fields stacked */}
              <span className="flex min-w-0 flex-1 flex-col gap-2">
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
                  <span className="font-mono">
                    <Input
                      value={route.modelId}
                      onChange={(e) => {
                        const updated = model.routes.map((r, i) =>
                          i === idx ? { ...r, modelId: e.target.value } : r,
                        );
                        onChange({ ...model, routes: updated });
                      }}
                      placeholder="Provider model ID"
                    />
                  </span>
                )}

                <Stack direction="horizontal" gap="snug">
                  <span className="flex-1">
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
                            i === idx
                              ? { ...r, inputCostPer1M: parseFloat(e.target.value) || 0 }
                              : r,
                          );
                          onChange({ ...model, routes: updated });
                        }}
                      />
                    </Field>
                  </span>
                  <span className="flex-1">
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
                  </span>
                </Stack>
              </span>

              {/* Right column: weight (weighted only) + toggle + delete */}
              <span className="flex shrink-0 flex-col items-end gap-2">
                {model.strategy === 'weighted' && (
                  <Stack direction="horizontal" align="center" gap="tight">
                    <span className="w-16 text-center">
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
                    </span>
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
                  <button
                    type="button"
                    onClick={() =>
                      onChange({ ...model, routes: model.routes.filter((_, i) => i !== idx) })
                    }
                    className="rounded p-1.5 text-muted transition-colors hover:bg-surface hover:text-status-error"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </Stack>
              </span>
            </section>
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
