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
  Stack,
  Text,
  Toggle,
} from '@orbit/terra';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { fmtCost, fmtDate } from '@orbit/utils';
import { ApiRoute } from '@orbit/lib/routing';
import type { ProviderModel, PricingEntry } from '../_types/catalog';
import { ProviderBadge } from './provider-badge';
import { providerLabel } from '../_utils/catalog-constants';

// ── Types ─────────────────────────────────────────────────────────────────────

const STATUS_OPTIONS = [
  { value: 'enabled', label: 'Enabled' },
  { value: 'disabled', label: 'Disabled' },
];

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

function Chips({ values }: { values: string[] }) {
  if (values.length === 0)
    return (
      <Text size="caption" color="muted">
        —
      </Text>
    );
  return (
    <Stack direction="horizontal" wrap gap="tight">
      {values.map((v) => (
        <Badge key={v} variant="outline" size="sm">
          {v}
        </Badge>
      ))}
    </Stack>
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
    </Stack>
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
                <Chips values={model.capabilities} />
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
            <Toggle
              checked={model.enabled}
              onChange={(v) => void onToggleEnabled(model.provider, model.modelId, v)}
            />
          </Stack>
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Stack direction="horizontal" gap="snug" justify="between" grow>
          {p?.has_override ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => void handleClearOverride()}
              disabled={clearing || saving}
            >
              {clearing ? 'Clearing…' : 'Clear override'}
            </Button>
          ) : (
            <Stack />
          )}
          <Stack direction="horizontal" gap="snug">
            <Button variant="outline" size="sm" onClick={onClose} disabled={saving || clearing}>
              Cancel
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={() => void handleSaveOverride()}
              disabled={saving || clearing || !p}
            >
              {saving ? 'Saving…' : 'Save override'}
            </Button>
          </Stack>
        </Stack>
      </ModalFooter>
    </Modal>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function ProviderCatalogSection({ models: initialModels }: ProviderCatalogSectionProps) {
  const [localModels, setLocalModels] = useState(initialModels);
  const [detailModel, setDetailModel] = useState<ProviderModel | null>(null);

  const providerOptions = useMemo(() => {
    const seen = new Set<string>();
    const opts: { value: string; label: string }[] = [];
    for (const m of localModels) {
      if (!seen.has(m.provider)) {
        seen.add(m.provider);
        opts.push({ value: m.provider, label: providerLabel(m.provider) });
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
        locked: true,
        sortable: true,
        sortAccessor: (m) => m.modelId,
        cell: (m) => (
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
        ),
      },
      {
        id: 'provider',
        header: 'Provider',
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
        sortable: true,
        sortAccessor: (m) => m.family,
        cell: (m) => (
          <Text size="supporting" color="muted" mono>
            {m.family || '—'}
          </Text>
        ),
      },
      {
        id: 'effectivePrice',
        header: 'Effective Price',
        sortable: true,
        sortAccessor: (m) => m.pricing?.effective_input_cost_per_1m ?? -1,
        cell: (m) => <EffectivePriceCell pricing={m.pricing} />,
      },
      {
        id: 'capabilities',
        header: 'Capabilities',
        cell: (m) => <Chips values={m.capabilities} />,
      },
      {
        id: 'input',
        header: 'Input',
        defaultVisible: false,
        cell: (m) => <Chips values={m.inputModalities} />,
      },
      {
        id: 'output',
        header: 'Output',
        defaultVisible: false,
        cell: (m) => <Chips values={m.outputModalities} />,
      },
      {
        id: 'dialects',
        header: 'Dialects',
        defaultVisible: false,
        cell: (m) => <Chips values={m.dialects} />,
      },
      {
        id: 'enabled',
        header: 'Enabled',
        locked: true,
        filter: {
          mode: 'client',
          options: STATUS_OPTIONS,
          filterFn: (m, selected) => selected.has(m.enabled ? 'enabled' : 'disabled'),
        },
        cell: (m) => (
          <Stack onClick={(e: React.MouseEvent) => e.stopPropagation()}>
            <Toggle checked={m.enabled} onChange={(v) => void toggle(m.provider, m.modelId, v)} />
          </Stack>
        ),
      },
    ],
    [toggle, providerOptions],
  );

  return (
    <>
      <SmartTable
        tableId="catalogProviders"
        data={localModels}
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
