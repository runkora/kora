'use client';

import { useState, useCallback, useMemo } from 'react';
import {
  Alert,
  Badge,
  Button,
  Icon,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  SegmentedControl,
  Stack,
  Text,
  Toggle,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@runkora/hades';
import { PermissionHint, SmartTable } from '@runkora/components';
import type { DataTableColumn } from '@runkora/components';
import { BadgeList } from '../../_components/badge-list';
import { fmtCost, fmtDate } from '@runkora/utils';
import { ApiRoute } from '@runkora/lib/routing';
import type { ProviderModel, PricingEntry } from '../_types/catalog';
import { ProviderBadge } from './provider-badge';
import { providerTitle } from '../_utils/catalog-constants';
import { useHasScope } from '../../../_components/permissions-provider';

// ── Types ─────────────────────────────────────────────────────────────────────

interface ProviderCatalogSectionProps {
  models: ProviderModel[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function providerModelMatchesSearch(m: ProviderModel, q: string): boolean {
  return (
    m.modelId.toLowerCase().includes(q) ||
    m.displayName.toLowerCase().includes(q) ||
    m.family.toLowerCase().includes(q)
  );
}

function hasTokenPricing(pricing: PricingEntry): boolean {
  return pricing.effective_input_cost_per_1m > 0 || pricing.effective_output_cost_per_1m > 0;
}

function hasNonTokenPricing(pricing: PricingEntry): boolean {
  return (
    (pricing.synced_image_input_cost_per_image ?? 0) > 0 ||
    (pricing.synced_image_output_cost_per_image ?? 0) > 0 ||
    (pricing.synced_request_input_cost ?? 0) > 0 ||
    (pricing.synced_request_output_cost ?? 0) > 0 ||
    (pricing.synced_second_input_cost ?? 0) > 0 ||
    (pricing.synced_second_output_cost ?? 0) > 0 ||
    (pricing.synced_audio_input_cost_per_1m ?? 0) > 0 ||
    (pricing.synced_audio_output_cost_per_1m ?? 0) > 0
  );
}

function EffectivePriceCell({ pricing }: { pricing?: PricingEntry }) {
  if (!pricing) {
    return (
      <Stack direction="horizontal" align="center" gap="tight">
        <Icon name="alert-circle" size="2xs" color="status-degraded" />
        <Text size="caption" color="muted">
          Unknown
        </Text>
      </Stack>
    );
  }

  const tokenPricing = hasTokenPricing(pricing);
  const nonTokenPricing = hasNonTokenPricing(pricing);

  if (!tokenPricing && nonTokenPricing) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="muted" size="icon-sm" aria-label="Non-token pricing — see model details">
            <Icon name="info" size="2xs" color="muted" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Non-token pricing — see model details</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <Stack gap="none">
      <Stack direction="horizontal" align="center" gap="tight">
        <Text size="supporting" mono color="foreground">
          {fmtCost(pricing.effective_input_cost_per_1m)}/1M
        </Text>
        {pricing.has_override && (
          <Badge variant="error" size="sm">
            override
          </Badge>
        )}
      </Stack>
      <Text size="caption" color="muted" mono>
        out: {fmtCost(pricing.effective_output_cost_per_1m)}/1M
      </Text>
      {nonTokenPricing && (
        <Text size="caption" color="muted">
          + other dims
        </Text>
      )}
    </Stack>
  );
}

// ── Non-token pricing rows ────────────────────────────────────────────────────

function NonTokenPricingRows({ pricing }: { pricing: PricingEntry }) {
  const dims: Array<{ label: string; value: number }> = [];
  if ((pricing.synced_image_input_cost_per_image ?? 0) > 0)
    dims.push({ label: 'Image input /image', value: pricing.synced_image_input_cost_per_image! });
  if ((pricing.synced_image_output_cost_per_image ?? 0) > 0)
    dims.push({ label: 'Image output /image', value: pricing.synced_image_output_cost_per_image! });
  if ((pricing.synced_request_input_cost ?? 0) > 0)
    dims.push({ label: 'Request input', value: pricing.synced_request_input_cost! });
  if ((pricing.synced_request_output_cost ?? 0) > 0)
    dims.push({ label: 'Request output', value: pricing.synced_request_output_cost! });
  if ((pricing.synced_second_input_cost ?? 0) > 0)
    dims.push({ label: 'Per-second input', value: pricing.synced_second_input_cost! });
  if ((pricing.synced_second_output_cost ?? 0) > 0)
    dims.push({ label: 'Per-second output', value: pricing.synced_second_output_cost! });
  if ((pricing.synced_audio_input_cost_per_1m ?? 0) > 0)
    dims.push({ label: 'Audio input /1M', value: pricing.synced_audio_input_cost_per_1m! });
  if ((pricing.synced_audio_output_cost_per_1m ?? 0) > 0)
    dims.push({ label: 'Audio output /1M', value: pricing.synced_audio_output_cost_per_1m! });

  if (dims.length === 0) return null;
  return (
    <>
      {dims.map(({ label, value }) => (
        <Stack key={label} direction="horizontal" align="center" justify="between">
          <Text size="supporting" color="muted">
            {label}
          </Text>
          <Text size="supporting" mono>
            {fmtCost(value)}
          </Text>
        </Stack>
      ))}
    </>
  );
}

// ── Detail overlay ────────────────────────────────────────────────────────────

interface PricingOverlayProps {
  model: ProviderModel;
  open: boolean;
  onClose: () => void;
  onModelChange: (m: ProviderModel) => void;
  onToggleEnabled: (provider: string, modelId: string, enabled: boolean) => Promise<void>;
}

function PricingOverlay({
  model,
  open,
  onClose,
  onModelChange,
  onToggleEnabled,
}: PricingOverlayProps) {
  const canWrite = useHasScope('platform:catalog:write');
  const [inputOverride, setInputOverride] = useState(
    model.pricing?.override_input_cost_per_1m != null
      ? String(model.pricing.override_input_cost_per_1m)
      : '',
  );
  const [outputOverride, setOutputOverride] = useState(
    model.pricing?.override_output_cost_per_1m != null
      ? String(model.pricing.override_output_cost_per_1m)
      : '',
  );
  const [saving, setSaving] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSaveOverride = useCallback(async () => {
    const inp = inputOverride.trim() !== '' ? parseFloat(inputOverride) : undefined;
    const out = outputOverride.trim() !== '' ? parseFloat(outputOverride) : undefined;

    if ((inp !== undefined && isNaN(inp)) || (out !== undefined && isNaN(out))) {
      setError('Override prices must be valid numbers.');
      return;
    }
    if (inp === undefined && out === undefined) {
      setError('At least one override price field is required.');
      return;
    }

    setSaving(true);
    setError(null);
    try {
      const body: Record<string, number> = {};
      if (inp !== undefined) body.input_cost_per_1m = inp;
      if (out !== undefined) body.output_cost_per_1m = out;

      const res = await fetch(ApiRoute.catalogPricingOverride.PUT(model.provider, model.modelId), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const b = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(b.message ?? `Request failed (${res.status})`);
      }
      onModelChange({
        ...model,
        pricing: model.pricing
          ? {
              ...model.pricing,
              override_input_cost_per_1m: inp ?? model.pricing.override_input_cost_per_1m,
              override_output_cost_per_1m: out ?? model.pricing.override_output_cost_per_1m,
              effective_input_cost_per_1m: inp ?? model.pricing.effective_input_cost_per_1m,
              effective_output_cost_per_1m: out ?? model.pricing.effective_output_cost_per_1m,
              has_override: true,
            }
          : undefined,
      });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save override.');
    } finally {
      setSaving(false);
    }
  }, [inputOverride, outputOverride, model, onModelChange, onClose]);

  const handleClearOverride = useCallback(async () => {
    setClearing(true);
    setError(null);
    try {
      const res = await fetch(
        ApiRoute.catalogPricingOverride.DELETE(model.provider, model.modelId),
        { method: 'DELETE' },
      );
      if (!res.ok) {
        const b = (await res.json().catch(() => ({}))) as { message?: string };
        throw new Error(b.message ?? `Request failed (${res.status})`);
      }
      setInputOverride('');
      setOutputOverride('');
      onModelChange({
        ...model,
        pricing: model.pricing
          ? {
              ...model.pricing,
              override_input_cost_per_1m: null,
              override_output_cost_per_1m: null,
              override_cache_read_cost_per_1m: null,
              override_cache_write_cost_per_1m: null,
              effective_input_cost_per_1m: model.pricing.synced_input_cost_per_1m,
              effective_output_cost_per_1m: model.pricing.synced_output_cost_per_1m,
              has_override: false,
            }
          : undefined,
      });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to clear override.');
    } finally {
      setClearing(false);
    }
  }, [model, onModelChange, onClose]);

  const p = model.pricing;
  const hasCachePricing =
    p &&
    ((p.synced_cache_read_cost_per_1m ?? 0) > 0 || (p.synced_cache_write_cost_per_1m ?? 0) > 0);

  return (
    <Modal open={open} onClose={onClose} size="sm">
      <ModalHeader title={model.displayName || model.modelId} />
      <ModalBody>
        <Stack gap="comfortable">
          {/* Read-only metadata */}
          <Stack gap="snug">
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="supporting" color="muted">
                Provider
              </Text>
              <ProviderBadge provider={model.provider} />
            </Stack>
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="supporting" color="muted">
                Model ID
              </Text>
              <Text size="supporting" mono>
                {model.modelId}
              </Text>
            </Stack>
            {model.family && (
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="supporting" color="muted">
                  Family
                </Text>
                <Text size="supporting" mono>
                  {model.family}
                </Text>
              </Stack>
            )}
            {model.capabilities.length > 0 && (
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="supporting" color="muted">
                  Capabilities
                </Text>
                <BadgeList values={model.capabilities} maxRows={2} />
              </Stack>
            )}
          </Stack>

          {/* Pricing metadata */}
          {p ? (
            <Stack gap="snug">
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="supporting" color="muted">
                  Last synced
                </Text>
                <Text size="supporting" color="muted">
                  {fmtDate(p.last_synced_at)}
                </Text>
              </Stack>
              {hasTokenPricing(p) && (
                <>
                  <Stack direction="horizontal" align="center" justify="between">
                    <Text size="supporting" color="muted">
                      Synced input /1M
                    </Text>
                    <Text size="supporting" mono>
                      {fmtCost(p.synced_input_cost_per_1m)}
                    </Text>
                  </Stack>
                  <Stack direction="horizontal" align="center" justify="between">
                    <Text size="supporting" color="muted">
                      Synced output /1M
                    </Text>
                    <Text size="supporting" mono>
                      {fmtCost(p.synced_output_cost_per_1m)}
                    </Text>
                  </Stack>
                </>
              )}
              <NonTokenPricingRows pricing={p} />
            </Stack>
          ) : (
            <Alert variant="warning">
              <Icon name="alert-circle" size="2xs" color="status-degraded" />
              <Text size="caption" color="muted">
                No pricing data available. This model must appear in the pricing catalog before
                overrides can be set.
              </Text>
            </Alert>
          )}

          {/* Cache pricing (only when non-zero) */}
          {hasCachePricing && (
            <Stack gap="snug">
              <Text size="supporting" weight="emphasis">
                Cache pricing
              </Text>
              {(p!.synced_cache_read_cost_per_1m ?? 0) > 0 && (
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Cache read /1M
                  </Text>
                  <Text size="supporting" mono>
                    {fmtCost(p?.synced_cache_read_cost_per_1m ?? 0)}
                  </Text>
                </Stack>
              )}
              {(p!.synced_cache_write_cost_per_1m ?? 0) > 0 && (
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Cache write /1M
                  </Text>
                  <Text size="supporting" mono>
                    {fmtCost(p?.synced_cache_write_cost_per_1m ?? 0)}
                  </Text>
                </Stack>
              )}
            </Stack>
          )}

          {/* Override inputs */}
          <Stack gap="snug">
            <Text size="supporting" weight="emphasis">
              Price overrides
            </Text>
            <Stack gap="snug">
              <Text size="caption" color="muted">
                Input /1M tokens (USD)
              </Text>
              <Input
                type="number"
                value={inputOverride}
                onChange={(e) => setInputOverride(e.target.value)}
                placeholder={p ? String(p.synced_input_cost_per_1m) : '0.00'}
                min="0"
                step="0.01"
                disabled={saving || clearing}
              />
            </Stack>
            <Stack gap="snug">
              <Text size="caption" color="muted">
                Output /1M tokens (USD)
              </Text>
              <Input
                type="number"
                value={outputOverride}
                onChange={(e) => setOutputOverride(e.target.value)}
                placeholder={p ? String(p.synced_output_cost_per_1m) : '0.00'}
                min="0"
                step="0.01"
                disabled={saving || clearing}
              />
            </Stack>
            {error && <Alert variant="error">{error}</Alert>}
          </Stack>

          {/* Enable/disable toggle */}
          <Stack direction="horizontal" align="center" justify="between">
            <Text size="supporting" color="muted">
              Enabled
            </Text>
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Toggle
                checked={model.enabled}
                onChange={(v) => void onToggleEnabled(model.provider, model.modelId, v)}
                disabled={!canWrite}
                aria-label={`Enable ${model.modelId}`}
              />
            </PermissionHint>
          </Stack>
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Stack direction="horizontal" gap="snug" justify="between" grow>
          {p?.has_override ? (
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void handleClearOverride()}
                disabled={!canWrite || clearing || saving}
              >
                {clearing ? 'Clearing…' : 'Clear override'}
              </Button>
            </PermissionHint>
          ) : (
            <Stack />
          )}
          <Stack direction="horizontal" gap="snug">
            <Button variant="outline" size="sm" onClick={onClose} disabled={saving || clearing}>
              Cancel
            </Button>
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Button
                variant="default"
                size="sm"
                onClick={() => void handleSaveOverride()}
                disabled={!canWrite || saving || clearing || !p}
              >
                {saving ? 'Saving…' : 'Save override'}
              </Button>
            </PermissionHint>
          </Stack>
        </Stack>
      </ModalFooter>
    </Modal>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function ProviderCatalogSection({ models: initialModels }: ProviderCatalogSectionProps) {
  const canWrite = useHasScope('platform:catalog:write');
  const [localModels, setLocalModels] = useState(initialModels);
  const [detailModel, setDetailModel] = useState<ProviderModel | null>(null);
  const [statusFilter, setStatusFilter] = useState<'all' | 'enabled' | 'disabled'>('all');

  const statusCounts = useMemo(
    () => ({
      all: localModels.length,
      enabled: localModels.filter((m) => m.enabled).length,
      disabled: localModels.filter((m) => !m.enabled).length,
    }),
    [localModels],
  );

  const filteredModels = useMemo(() => {
    if (statusFilter === 'all') return localModels;
    return localModels.filter((m) => (statusFilter === 'enabled') === m.enabled);
  }, [localModels, statusFilter]);

  const providerOptions = useMemo(() => {
    const seen = new Set<string>();
    const opts: { value: string; label: string }[] = [];
    for (const m of localModels) {
      if (!seen.has(m.provider)) {
        seen.add(m.provider);
        opts.push({ value: m.provider, label: providerTitle(m.provider) });
      }
    }
    return opts.sort((a, b) => a.label.localeCompare(b.label));
  }, [localModels]);

  const familyOptions = useMemo(() => {
    const seen = new Set<string>();
    const opts: { value: string; label: string }[] = [];
    for (const m of localModels) {
      if (m.family && !seen.has(m.family)) {
        seen.add(m.family);
        opts.push({ value: m.family, label: m.family });
      }
    }
    return opts.sort((a, b) => a.label.localeCompare(b.label));
  }, [localModels]);

  const capabilityOptions = useMemo(() => {
    const seen = new Set<string>();
    const opts: { value: string; label: string }[] = [];
    for (const m of localModels) {
      for (const c of m.capabilities) {
        if (!seen.has(c)) {
          seen.add(c);
          opts.push({ value: c, label: c });
        }
      }
    }
    return opts.sort((a, b) => a.label.localeCompare(b.label));
  }, [localModels]);

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

  const handleModelChange = useCallback((updated: ProviderModel) => {
    setLocalModels((prev) =>
      prev.map((m) =>
        m.provider === updated.provider && m.modelId === updated.modelId ? updated : m,
      ),
    );
    setDetailModel(updated);
  }, []);

  const columns = useMemo<DataTableColumn<ProviderModel>[]>(
    () => [
      {
        id: 'modelId',
        header: 'Model ID',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '10rem',
        locked: true,
        // IDs share long prefixes and differ at the end, which truncation cuts
        // off, so they wrap instead and are always readable in full.
        wrap: true,
        sortable: true,
        sortAccessor: (m) => m.modelId,
        cell: (m) => (
          <Stack gap="none">
            <Text
              size="supporting"
              weight={m.enabled ? 'emphasis' : 'regular'}
              mono
              style={{ overflowWrap: 'anywhere' }}
            >
              {m.modelId}
            </Text>
            {m.displayName && (
              <Text size="caption" color="muted">
                {m.displayName}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'provider',
        header: 'Provider',
        gridWidth: '9rem',
        // The widest label, "AWS Bedrock (aus)": truncating it drops the account alias.
        minWidth: '8.5rem',
        sortable: true,
        sortAccessor: (m) => m.provider,
        filter: {
          mode: 'client',
          options: providerOptions,
          filterFn: (m, selected) => selected.has(m.provider),
        },
        cell: (m) => <ProviderBadge provider={m.provider} />,
      },
      {
        id: 'family',
        header: 'Family',
        gridWidth: '8rem',
        minWidth: '5rem',
        sortable: true,
        sortAccessor: (m) => m.family,
        filter: {
          mode: 'client',
          options: familyOptions,
          filterFn: (m, selected) => selected.has(m.family),
        },
        cell: (m) => (
          <Text size="supporting" color="muted" mono>
            {m.family || '—'}
          </Text>
        ),
      },
      {
        id: 'effectivePrice',
        header: 'Effective Price',
        gridWidth: '9rem',
        minWidth: '7.5rem',
        headerTooltip:
          'The per-1M token price for a standard text request, after any platform overrides are applied.',
        sortable: true,
        sortAccessor: (m) =>
          m.pricing
            ? (m.pricing.effective_input_cost_per_1m ?? 0) +
              (m.pricing.effective_output_cost_per_1m ?? 0)
            : -1,
        cell: (m) => <EffectivePriceCell pricing={m.pricing} />,
      },
      {
        id: 'capabilities',
        header: 'Capabilities',
        gridWidth: 'minmax(0, 1.5fr)',
        minWidth: '6rem',
        filter: {
          mode: 'client',
          options: capabilityOptions,
          filterFn: (m, selected) => [...selected].some((c) => m.capabilities.includes(c)),
        },
        cell: (m) => <BadgeList values={m.capabilities} maxRows={2} />,
      },
      {
        id: 'input',
        header: 'Input',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '4.5rem',
        defaultVisible: false,
        cell: (m) => <BadgeList values={m.inputModalities} maxRows={2} />,
      },
      {
        id: 'output',
        header: 'Output',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '4.5rem',
        defaultVisible: false,
        cell: (m) => <BadgeList values={m.outputModalities} maxRows={2} />,
      },
      {
        id: 'dialects',
        header: 'Dialects',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '4.5rem',
        defaultVisible: false,
        cell: (m) => <BadgeList values={m.dialects} maxRows={2} />,
      },
      {
        id: 'enabled',
        header: 'Enabled',
        gridWidth: '5.5rem',
        minWidth: '4.5rem',
        locked: true,
        cell: (m) => (
          <Stack onClick={(e: React.MouseEvent) => e.stopPropagation()}>
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Toggle
                checked={m.enabled}
                onChange={(v) => void toggle(m.provider, m.modelId, v)}
                disabled={!canWrite}
                aria-label={`Enable ${m.modelId}`}
              />
            </PermissionHint>
          </Stack>
        ),
      },
    ],
    [toggle, providerOptions, familyOptions, capabilityOptions, canWrite],
  );

  return (
    <>
      <SmartTable
        tableId="catalogProviders"
        data={filteredModels}
        columns={columns}
        getRowKey={(m) => `${m.provider}/${m.modelId}`}
        noun="provider models"
        searchPlaceholder="Search models…"
        searchFn={providerModelMatchesSearch}
        defaultSortId="modelId"
        defaultSortDir="asc"
        emptyMessage="No provider models discovered yet."
        filteredEmptyMessage="No provider models match your filters."
        onRowClick={setDetailModel}
        quickFilters={
          <SegmentedControl
            aria-label="Filter provider models by status"
            value={statusFilter}
            onChange={(v) => setStatusFilter(v as typeof statusFilter)}
            options={[
              { value: 'all', label: 'All', count: statusCounts.all },
              { value: 'enabled', label: 'Enabled', count: statusCounts.enabled },
              { value: 'disabled', label: 'Disabled', count: statusCounts.disabled },
            ]}
          />
        }
      />

      {detailModel && (
        <PricingOverlay
          model={detailModel}
          open
          onClose={() => setDetailModel(null)}
          onModelChange={handleModelChange}
          onToggleEnabled={toggle}
        />
      )}
    </>
  );
}
