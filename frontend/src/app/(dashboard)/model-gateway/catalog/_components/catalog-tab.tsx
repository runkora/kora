'use client';

import { useState, useCallback, useRef } from 'react';
import {
  AlertCircle,
  ArrowUpDown,
  ChevronRight,
  CircleDot,
  DollarSign,
  GripVertical,
  Layers,
  Network,
  Plus,
  Search,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  Field,
  Grid,
  Heading,
  Input,
  Label,
  Separator,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import type {
  VirtualModel as ApiVirtualModel,
  ProviderModel as ApiProviderModel,
} from '../../_services/catalog-server';

// ── Types ─────────────────────────────────────────────────────────────────────

type Provider = 'bedrock' | 'vertexai' | 'azure';
type RoutingStrategy = 'priority' | 'weighted' | 'lowest_cost';

interface RouteTarget {
  id: string;
  provider: Provider;
  modelId: string;
  priority: number;
  weight: number;
  inputCostPer1M: number;
  outputCostPer1M: number;
  enabled: boolean;
}

interface VirtualModel {
  id: string;
  family: string;
  name: string;
  displayName: string;
  description: string;
  strategy: RoutingStrategy;
  routes: RouteTarget[];
  enabled: boolean;
}

interface ProviderModel {
  provider: Provider;
  modelId: string;
  displayName: string;
  family: string;
  capabilities: string[];
  aliases: string[];
  inputModalities: string[];
  outputModalities: string[];
  dialects: string[];
  enabled: boolean;
}

// ── API adapters ──────────────────────────────────────────────────────────────

function toVirtualModel(m: ApiVirtualModel): VirtualModel {
  return {
    id: m.id,
    family: m.family,
    name: m.name,
    displayName: m.display_name,
    description: m.description,
    strategy: m.strategy as RoutingStrategy,
    enabled: m.enabled,
    routes: m.routes.map((r) => ({
      id: r.id,
      provider: r.provider as Provider,
      modelId: r.model_id,
      priority: r.priority,
      weight: r.weight,
      inputCostPer1M: r.input_cost_per_1m,
      outputCostPer1M: r.output_cost_per_1m,
      enabled: r.enabled,
    })),
  };
}

function toProviderModel(m: ApiProviderModel): ProviderModel {
  return {
    provider: m.provider as Provider,
    modelId: m.model_id,
    displayName: m.display_name,
    family: m.family,
    capabilities: m.capabilities,
    aliases: m.aliases,
    inputModalities: m.input_modalities ?? [],
    outputModalities: m.output_modalities ?? [],
    dialects: m.dialects ?? [],
    enabled: m.enabled,
  };
}

function vmToApiPayload(vm: VirtualModel, partial?: Partial<VirtualModel>) {
  const merged = { ...vm, ...partial };
  return {
    family: merged.family,
    name: merged.name,
    display_name: merged.displayName,
    description: merged.description,
    strategy: merged.strategy,
    enabled: merged.enabled,
    routes: merged.routes.map((r) => ({
      id: r.id,
      provider: r.provider,
      model_id: r.modelId,
      priority: r.priority,
      weight: r.weight,
      input_cost_per_1m: r.inputCostPer1M,
      output_cost_per_1m: r.outputCostPer1M,
      enabled: r.enabled,
    })),
  };
}

// ── Derived helpers ───────────────────────────────────────────────────────────

function vmFullId(vm: Pick<VirtualModel, 'family' | 'name'>): string {
  return `${vm.family}/${vm.name}`;
}

function vmPolicyScope(vm: Pick<VirtualModel, 'family' | 'name'>): string {
  return `inference:${vm.family}:${vm.name}:use`;
}

// ── Shared UI helpers ─────────────────────────────────────────────────────────

const PROVIDER_LABELS: Record<Provider, string> = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
};

const PROVIDER_COLORS: Record<Provider, string> = {
  bedrock: 'bg-[#ff9900]/15 text-[#ff9900]',
  vertexai: 'bg-[#4285f4]/15 text-[#4285f4]',
  azure: 'bg-[#0078d4]/15 text-[#0078d4]',
};

const STRATEGY_LABELS: Record<RoutingStrategy, string> = {
  priority: 'Priority / Failover',
  weighted: 'Weighted Split',
  lowest_cost: 'Lowest Cost',
};

const STRATEGY_COLORS: Record<RoutingStrategy, string> = {
  priority: 'bg-accent/15 text-accent',
  weighted: 'bg-[#a855f7]/15 text-[#a855f7]',
  lowest_cost: 'bg-status-healthy/15 text-status-healthy',
};

// Consistent select styling to match the Terra Input height and appearance.
const selectCls =
  'h-9 w-full rounded-md border border-border bg-surface-elevated px-3 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

function ProviderBadge({ provider }: { provider: Provider }) {
  return (
    <span
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${PROVIDER_COLORS[provider]}`}
    >
      {PROVIDER_LABELS[provider]}
    </span>
  );
}

function StrategyBadge({ strategy }: { strategy: RoutingStrategy }) {
  return (
    <span
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${STRATEGY_COLORS[strategy]}`}
    >
      {STRATEGY_LABELS[strategy]}
    </span>
  );
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={[
        'relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent',
        'transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        checked ? 'bg-accent' : 'bg-surface-elevated',
      ].join(' ')}
    >
      <span
        className={[
          'pointer-events-none inline-block h-4 w-4 rounded-full bg-foreground shadow transition-transform',
          checked ? 'translate-x-4' : 'translate-x-0',
        ].join(' ')}
      />
    </button>
  );
}

// ── KPI Card ──────────────────────────────────────────────────────────────────

function StatCard({
  icon,
  label,
  value,
  sub,
}: {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  sub?: string;
}) {
  return (
    <Card column>
      <CardHeader compact icon={icon}>
        <CardTitle>{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="none">
          <Heading variant="metric">{value}</Heading>
          {sub && (
            <Text size="caption" color="muted">
              {sub}
            </Text>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

// ── Routes & Strategy Tab ─────────────────────────────────────────────────────

function RoutesTab({
  model,
  onChange,
  providerModels,
}: {
  model: VirtualModel;
  onChange: (m: VirtualModel) => void;
  providerModels: ProviderModel[];
}) {
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
                    id: '',
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
                <select
                  value={route.provider}
                  onChange={(e) => {
                    const updated = model.routes.map((r, i) =>
                      i === idx ? { ...r, provider: e.target.value as Provider, modelId: '' } : r,
                    );
                    onChange({ ...model, routes: updated });
                  }}
                  className={selectCls}
                >
                  {providersWithModels.map((p) => (
                    <option key={p} value={p}>
                      {PROVIDER_LABELS[p] ?? p}
                    </option>
                  ))}
                </select>

                {modelsForProvider(route.provider).length > 0 ? (
                  <select
                    value={route.modelId}
                    onChange={(e) => {
                      const updated = model.routes.map((r, i) =>
                        i === idx ? { ...r, modelId: e.target.value } : r,
                      );
                      onChange({ ...model, routes: updated });
                    }}
                    className={`${selectCls} font-mono`}
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
                  </select>
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

// ── Editor Panel ──────────────────────────────────────────────────────────────

function EditorPanel({
  model,
  onClose,
  onSaved,
  providerModels,
}: {
  model: VirtualModel | null;
  onClose: () => void;
  onSaved: (vm: VirtualModel) => void;
  providerModels: ProviderModel[];
}) {
  const [draft, setDraft] = useState<VirtualModel | null>(model);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const knownFamilies = Array.from(new Set(providerModels.map((m) => m.family)))
    .filter(Boolean)
    .sort();

  if (model && draft && model.id !== draft.id) {
    setDraft(model);
  }

  if (!draft) return null;

  const isNew = draft.id === '__new__';
  const fullId = draft.family && draft.name ? vmFullId(draft) : null;

  async function handleSave() {
    if (!draft) return;
    setSaving(true);
    setError(null);
    try {
      const payload = vmToApiPayload(draft);
      const url = isNew
        ? '/api/catalog/virtual-models'
        : `/api/catalog/virtual-models/${encodeURIComponent(draft.id)}`;
      const method = isNew ? 'POST' : 'PUT';
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        setError(body.message ?? 'Save failed');
        return;
      }
      const body = (await res.json()) as { data: ApiVirtualModel };
      onSaved(toVirtualModel(body.data));
      onClose();
    } catch {
      setError('Network error');
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <span className="fixed inset-0 z-40 bg-black/40" onClick={onClose} aria-hidden />
      <section className="fixed inset-y-0 right-0 z-50 flex w-[580px] flex-col bg-surface shadow-2xl">
        {/* Header */}
        <header className="flex items-center justify-between border-b border-border px-5 py-4">
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" gap="snug">
              <Heading variant="section">{isNew ? 'New Virtual Model' : draft.displayName}</Heading>
              {!isNew && (
                <span
                  className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                    draft.enabled
                      ? 'bg-status-healthy/15 text-status-healthy'
                      : 'bg-muted/20 text-muted'
                  }`}
                >
                  {draft.enabled ? 'Enabled' : 'Disabled'}
                </span>
              )}
            </Stack>
            {fullId && (
              <Text size="caption" color="muted" mono>
                {fullId}
              </Text>
            )}
          </Stack>
          <button
            type="button"
            onClick={onClose}
            className="rounded p-1.5 text-muted transition-colors hover:bg-surface-elevated hover:text-foreground"
          >
            <X className="size-4" />
          </button>
        </header>

        {/* Identity fields */}
        <section className="border-b border-border px-5 py-4">
          <Grid cols={2} gap="snug">
            <Field>
              <Label>Family</Label>
              {knownFamilies.length > 0 ? (
                <select
                  value={draft.family}
                  onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                  className={`${selectCls} font-mono`}
                >
                  <option value="">— select —</option>
                  {knownFamilies.map((f) => (
                    <option key={f} value={f}>
                      {f}
                    </option>
                  ))}
                </select>
              ) : (
                <span className="font-mono">
                  <Input
                    value={draft.family}
                    onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                    placeholder="e.g. anthropic"
                  />
                </span>
              )}
            </Field>

            <Field>
              <Label>Model name</Label>
              <span className="font-mono">
                <Input
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  placeholder="e.g. claude-sonnet-4-6"
                />
              </span>
            </Field>

            <Field>
              <Label>Display name</Label>
              <Input
                value={draft.displayName}
                onChange={(e) => setDraft({ ...draft, displayName: e.target.value })}
                placeholder="e.g. Claude Sonnet 4.6"
              />
            </Field>

            <Field>
              <Label>Scope (derived)</Label>
              <span className="flex h-9 items-center rounded-md border border-border bg-surface px-3">
                <span className="truncate">
                  <Text size="caption" color="muted" mono>
                    {fullId ? vmPolicyScope(draft) : '—'}
                  </Text>
                </span>
              </span>
            </Field>

            <span className="col-span-2">
              <Field>
                <Label>Description</Label>
                <Input
                  value={draft.description}
                  onChange={(e) => setDraft({ ...draft, description: e.target.value })}
                  placeholder="Optional description"
                />
              </Field>
            </span>
          </Grid>
        </section>

        {/* Routes content */}
        <section className="flex-1 overflow-y-auto px-5 py-4">
          <RoutesTab model={draft} onChange={setDraft} providerModels={providerModels} />
        </section>

        {error && (
          <span className="mx-5 mb-3 block">
            <Alert variant="error">
              <AlertCircle className="size-3.5 shrink-0 text-status-error" />
              <Text size="caption" color="muted">
                {error}
              </Text>
            </Alert>
          </span>
        )}

        {/* Footer */}
        <footer className="flex items-center justify-between border-t border-border px-5 py-4">
          <Stack direction="horizontal" align="center" gap="snug">
            <Toggle checked={draft.enabled} onChange={(v) => setDraft({ ...draft, enabled: v })} />
            <Text size="supporting" color={draft.enabled ? 'foreground' : 'muted'}>
              {draft.enabled ? 'Enabled' : 'Disabled'}
            </Text>
          </Stack>
          <Stack direction="horizontal" gap="snug">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={() => void handleSave()} disabled={saving}>
              {saving ? 'Saving…' : 'Save Changes'}
            </Button>
          </Stack>
        </footer>
      </section>
    </>
  );
}

// ── Virtual Models Table ──────────────────────────────────────────────────────

function VirtualModelsSection({
  models,
  onSelect,
}: {
  models: VirtualModel[];
  onSelect: (m: VirtualModel) => void;
}) {
  const [search, setSearch] = useState('');

  const filtered = models.filter((m) => {
    if (!search) return true;
    const q = search.toLowerCase();
    return m.family.includes(q) || m.name.includes(q) || m.displayName.toLowerCase().includes(q);
  });

  const families = Array.from(new Set(filtered.map((m) => m.family))).sort();

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" gap="snug">
        <span className="relative flex-1 max-w-xs [&>input]:pl-9">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search virtual models…"
          />
        </span>
        <Button
          size="sm"
          onClick={() =>
            onSelect({
              id: '__new__',
              family: '',
              name: '',
              displayName: '',
              description: '',
              strategy: 'priority',
              routes: [],
              enabled: true,
            })
          }
        >
          <Plus className="mr-1.5 size-3.5" />
          New Virtual Model
        </Button>
      </Stack>

      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Virtual Model</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Routes</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {models.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5}>
                      <figure className="py-12 text-center m-0">
                        <Stack gap="snug" align="center">
                          <Layers className="size-8 text-muted" />
                          <Text size="supporting" color="muted">
                            No virtual models configured
                          </Text>
                          <Text size="caption" color="muted">
                            Create your first virtual model to start routing inference requests.
                          </Text>
                        </Stack>
                      </figure>
                    </TableCell>
                  </TableRow>
                ) : (
                  families.map((family) =>
                    filtered
                      .filter((m) => m.family === family)
                      .map((m) => (
                        <TableRow
                          key={m.id}
                          style={{ cursor: 'pointer' }}
                          onClick={() => onSelect(m)}
                        >
                          <TableCell>
                            <Stack gap="none">
                              <span className="mb-0.5 block text-[10px] uppercase tracking-wider">
                                <Text size="caption" color="subtle">
                                  {family}
                                </Text>
                              </span>
                              <Text size="supporting" weight="emphasis" mono>
                                {m.name}
                              </Text>
                              <Text size="caption" color="muted">
                                {m.displayName}
                              </Text>
                            </Stack>
                          </TableCell>
                          <TableCell>
                            <StrategyBadge strategy={m.strategy} />
                          </TableCell>
                          <TableCell>
                            <Stack direction="horizontal" align="center" gap="tight">
                              <Text size="supporting">{m.routes.length}</Text>
                              <span className="flex gap-0.5">
                                {m.routes.map((r, ri) => (
                                  <span
                                    key={r.id || ri}
                                    className={`h-1.5 w-1.5 rounded-full ${r.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
                                    title={`${PROVIDER_LABELS[r.provider] ?? r.provider} — ${r.enabled ? 'active' : 'disabled'}`}
                                  />
                                ))}
                              </span>
                            </Stack>
                          </TableCell>
                          <TableCell>
                            <span
                              className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
                                m.enabled
                                  ? 'bg-status-healthy/15 text-status-healthy'
                                  : 'bg-muted/20 text-muted'
                              }`}
                            >
                              <span
                                className={`h-1.5 w-1.5 rounded-full ${m.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
                              />
                              {m.enabled ? 'Enabled' : 'Disabled'}
                            </span>
                          </TableCell>
                          <TableCell>
                            <ChevronRight className="size-4 text-muted" />
                          </TableCell>
                        </TableRow>
                      )),
                  )
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>
    </Stack>
  );
}

// ── Provider Catalog Section ──────────────────────────────────────────────────

function ProviderCatalogSection({ models }: { models: ProviderModel[] }) {
  const [search, setSearch] = useState('');
  const [providerFilter, setProviderFilter] = useState<Provider | 'all'>('all');
  const [hideDisabled, setHideDisabled] = useState(false);
  const [localModels, setLocalModels] = useState(models);

  const filtered = localModels.filter((m) => {
    if (hideDisabled && !m.enabled) return false;
    if (providerFilter !== 'all' && m.provider !== providerFilter) return false;
    if (search) {
      const q = search.toLowerCase();
      if (
        !m.displayName.toLowerCase().includes(q) &&
        !m.family.toLowerCase().includes(q) &&
        !m.modelId.toLowerCase().includes(q)
      )
        return false;
    }
    return true;
  });

  const toggle = useCallback(async (provider: string, modelId: string, enabled: boolean) => {
    setLocalModels((prev) =>
      prev.map((m) => (m.provider === provider && m.modelId === modelId ? { ...m, enabled } : m)),
    );
    try {
      const res = await fetch(
        `/api/catalog/provider-models/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled }),
        },
      );
      if (!res.ok) {
        setLocalModels((prev) =>
          prev.map((m) =>
            m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
          ),
        );
      }
    } catch {
      setLocalModels((prev) =>
        prev.map((m) =>
          m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
        ),
      );
    }
  }, []);

  const providers: Provider[] = ['bedrock', 'vertexai', 'azure'];

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <span className="relative max-w-xs flex-1 [&>input]:pl-9">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter models…"
          />
        </span>

        <Stack direction="horizontal" gap="tight">
          <Button
            variant={providerFilter === 'all' ? 'secondary' : 'ghost'}
            size="sm"
            onClick={() => setProviderFilter('all')}
          >
            All
          </Button>
          {providers.map((p) => (
            <Button
              key={p}
              variant={providerFilter === p ? 'secondary' : 'ghost'}
              size="sm"
              onClick={() => setProviderFilter(providerFilter === p ? 'all' : p)}
            >
              {PROVIDER_LABELS[p]}
            </Button>
          ))}
        </Stack>

        <Button
          variant={hideDisabled ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setHideDisabled(!hideDisabled)}
        >
          {hideDisabled ? 'Show all' : 'Hide disabled'}
        </Button>
      </Stack>

      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Model ID</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Family</TableHead>
                  <TableHead>Input</TableHead>
                  <TableHead>Output</TableHead>
                  <TableHead>Dialects</TableHead>
                  <TableHead>Capabilities</TableHead>
                  <TableHead>Enabled</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {localModels.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8}>
                      <figure className="py-12 text-center m-0">
                        <Stack gap="snug" align="center">
                          <CircleDot className="size-8 text-muted" />
                          <Text size="supporting" color="muted">
                            No provider models discovered yet
                          </Text>
                          <Text size="caption" color="muted">
                            Provider models appear here after the gateway performs its first
                            discovery sweep.
                          </Text>
                        </Stack>
                      </figure>
                    </TableCell>
                  </TableRow>
                ) : (
                  filtered.map((m) => (
                    <TableRow
                      key={`${m.provider}/${m.modelId}`}
                      style={{ opacity: m.enabled ? 1 : 0.5 }}
                    >
                      <TableCell>
                        <Stack gap="none">
                          <Text size="supporting" weight={m.enabled ? 'emphasis' : 'regular'} mono>
                            {m.modelId}
                          </Text>
                          {m.displayName && (
                            <Text size="caption" color="muted">
                              {m.displayName}
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <ProviderBadge provider={m.provider} />
                      </TableCell>
                      <TableCell>
                        <Text size="supporting" color="muted" mono>
                          {m.family || '—'}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.inputModalities.length > 0 ? (
                            m.inputModalities.map((mod) => (
                              <Badge key={mod} variant="outline" size="sm">
                                {mod}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.outputModalities.length > 0 ? (
                            m.outputModalities.map((mod) => (
                              <Badge key={mod} variant="outline" size="sm">
                                {mod}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.dialects.length > 0 ? (
                            m.dialects.map((d) => (
                              <Badge key={d} variant="outline" size="sm">
                                {d}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.capabilities.length > 0 ? (
                            m.capabilities.map((c) => (
                              <Badge key={c} variant="outline" size="sm">
                                {c}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Toggle
                          checked={m.enabled}
                          onChange={(v) => void toggle(m.provider, m.modelId, v)}
                        />
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
        {localModels.length > 0 && (
          <CardFooter compact justify="between">
            <Text size="caption" color="muted">
              {localModels.filter((m) => m.enabled).length} of {localModels.length} provider models
              enabled
            </Text>
            <Text size="caption" color="muted">
              Disabled models are excluded from routing
            </Text>
          </CardFooter>
        )}
      </Card>
    </Stack>
  );
}

// ── Root ──────────────────────────────────────────────────────────────────────

interface CatalogTabProps {
  initialVirtualModels: ApiVirtualModel[];
  initialProviderModels: ApiProviderModel[];
  initialError?: string | null;
}

export function CatalogTab({
  initialVirtualModels,
  initialProviderModels,
  initialError,
}: CatalogTabProps) {
  const [selectedModel, setSelectedModel] = useState<VirtualModel | null>(null);
  const [virtualModels, setVirtualModels] = useState<VirtualModel[]>(
    initialVirtualModels.map(toVirtualModel),
  );
  const providerModels = initialProviderModels.map(toProviderModel);

  const handleModelSaved = useCallback((saved: VirtualModel) => {
    setVirtualModels((prev) => {
      const idx = prev.findIndex((m) => m.id === saved.id);
      if (idx >= 0) {
        const updated = [...prev];
        updated[idx] = saved;
        return updated;
      }
      return [...prev, saved];
    });
  }, []);

  const enabledVMs = virtualModels.filter((m) => m.enabled).length;
  const enabledPMs = providerModels.filter((m) => m.enabled).length;
  const connectedProviders = new Set(providerModels.map((m) => m.provider)).size;

  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <Stack direction="horizontal" align="center" gap="snug">
          <Heading variant="title">Model Catalog</Heading>
          <Badge variant="outline" size="sm">
            Platform Admin
          </Badge>
        </Stack>
        <Text size="supporting" color="muted">
          Manage virtual models, routing rules, and provider access. Changes take effect immediately
          — no gateway restart required.
        </Text>
        {initialError && (
          <Alert variant="error">
            <AlertCircle className="size-3.5 shrink-0 text-status-error" />
            <Text size="caption" color="muted">
              Failed to load catalog: {initialError}
            </Text>
          </Alert>
        )}
      </Stack>

      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        <StatCard
          icon={<Layers className="size-4" />}
          label="Virtual Models"
          value={virtualModels.length}
          sub={`${enabledVMs} enabled`}
        />
        <StatCard
          icon={<CircleDot className="size-4" />}
          label="Provider Models"
          value={providerModels.length}
          sub={`${enabledPMs} enabled`}
        />
        <StatCard
          icon={<Network className="size-4" />}
          label="Connected Providers"
          value={connectedProviders || '—'}
          sub={connectedProviders > 0 ? 'discovered' : 'none yet'}
        />
        <StatCard
          icon={<ShieldCheck className="size-4" />}
          label="Policy Scopes"
          value={virtualModels.length}
          sub="auto-registered"
        />
      </Grid>

      <Tabs defaultValue="virtual">
        <TabsList>
          <TabsTrigger value="virtual">
            Virtual Models
            <span className="ml-2 rounded-full bg-surface-elevated px-1.5 py-0.5 text-[11px] text-muted">
              {virtualModels.length}
            </span>
          </TabsTrigger>
          <TabsTrigger value="provider">
            Provider Catalog
            <span className="ml-2 rounded-full bg-surface-elevated px-1.5 py-0.5 text-[11px] text-muted">
              {providerModels.length}
            </span>
          </TabsTrigger>
        </TabsList>
        <TabsContent value="virtual">
          <VirtualModelsSection models={virtualModels} onSelect={setSelectedModel} />
        </TabsContent>
        <TabsContent value="provider">
          <ProviderCatalogSection models={providerModels} />
        </TabsContent>
      </Tabs>

      {selectedModel && (
        <EditorPanel
          model={selectedModel}
          onClose={() => setSelectedModel(null)}
          onSaved={handleModelSaved}
          providerModels={providerModels}
        />
      )}
    </Stack>
  );
}
