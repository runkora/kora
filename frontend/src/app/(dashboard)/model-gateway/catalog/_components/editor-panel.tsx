'use client';

import { useState, useEffect } from 'react';
import {
  Alert,
  Button,
  Field,
  Grid,
  Heading,
  Input,
  Label,
  NativeSelect,
  Stack,
  Text,
  Toggle,
  Icon,
} from '@orbit/terra';
import type { VirtualModel, ProviderModel } from '../_types/catalog';
import type { VirtualModel as ApiVirtualModel } from '../../_services/catalog-server';
import { vmFullId, vmPolicyScope, vmToApiPayload, toVirtualModel } from '../_utils/catalog-api';
import { RoutesTab } from './routes-tab';

interface EditorPanelProps {
  model: VirtualModel | null;
  onClose: () => void;
  onSaved: (vm: VirtualModel) => void;
  providerModels: ProviderModel[];
}

export function EditorPanel({ model, onClose, onSaved, providerModels }: EditorPanelProps) {
  const [draft, setDraft] = useState<VirtualModel | null>(model);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const knownFamilies = Array.from(new Set(providerModels.map((m) => m.family)))
    .filter(Boolean)
    .sort();

  // Sync draft when the parent swaps to a different model (e.g. user clicks a
  // different row while the panel is open). Must be in useEffect — calling
  // setState during render triggers React warnings and potential render loops.
  useEffect(() => {
    if (model) setDraft(model);
  }, [model?.id]); // eslint-disable-line react-hooks/exhaustive-deps

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
      {/* eslint-disable-next-line local/no-raw-div */}
      <div className="fixed inset-0 z-40 bg-black/40" onClick={onClose} aria-hidden />
      {/* eslint-disable-next-line local/no-raw-div */}
      <div className="fixed inset-y-0 right-0 z-50 flex w-[580px] flex-col bg-surface shadow-2xl">
        {/* Header */}
        {/* eslint-disable-next-line local/no-raw-div */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <Stack gap="tight">
            <Stack direction="horizontal" align="center" gap="snug">
              <Heading variant="section">{isNew ? 'New Virtual Model' : draft.displayName}</Heading>
              {!isNew && (
                // eslint-disable-next-line local/no-raw-div
                <div
                  className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                    draft.enabled
                      ? 'bg-status-healthy/15 text-status-healthy'
                      : 'bg-muted/20 text-muted'
                  }`}
                >
                  {draft.enabled ? 'Enabled' : 'Disabled'}
                </div>
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
            <Icon name="close" />
          </button>
        </div>

        {/* Identity fields */}
        {/* eslint-disable-next-line local/no-raw-div */}
        <div className="border-b border-border px-5 py-4">
          <Grid cols={2} gap="snug">
            <Field>
              <Label>Family</Label>
              {knownFamilies.length > 0 ? (
                <NativeSelect
                  value={draft.family}
                  onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                  mono
                >
                  <option value="">— select —</option>
                  {knownFamilies.map((f) => (
                    <option key={f} value={f}>
                      {f}
                    </option>
                  ))}
                </NativeSelect>
              ) : (
                // eslint-disable-next-line local/no-raw-div
                <div className="font-mono">
                  <Input
                    value={draft.family}
                    onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                    placeholder="e.g. anthropic"
                  />
                </div>
              )}
            </Field>

            <Field>
              <Label>Model name</Label>
              {/* eslint-disable-next-line local/no-raw-div */}
              <div className="font-mono">
                <Input
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  placeholder="e.g. claude-sonnet-4-6"
                />
              </div>
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
              {/* eslint-disable-next-line local/no-raw-div */}
              <div className="flex h-9 items-center rounded-md border border-border bg-surface px-3">
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="truncate">
                  <Text size="caption" color="muted" mono>
                    {fullId ? vmPolicyScope(draft) : '—'}
                  </Text>
                </div>
              </div>
            </Field>

            {/* eslint-disable-next-line local/no-raw-div */}
            <div className="col-span-2">
              <Field>
                <Label>Description</Label>
                <Input
                  value={draft.description}
                  onChange={(e) => setDraft({ ...draft, description: e.target.value })}
                  placeholder="Optional description"
                />
              </Field>
            </div>
          </Grid>
        </div>

        {/* Routes content */}
        {/* eslint-disable-next-line local/no-raw-div */}
        <div className="flex-1 overflow-y-auto px-5 py-4">
          <RoutesTab model={draft} onChange={setDraft} providerModels={providerModels} />
        </div>

        {error && (
          // eslint-disable-next-line local/no-raw-div
          <div className="mx-5 mb-3">
            <Alert variant="error">
              <Icon name="alert-circle" size="2xs" color="status-error" />
              <Text size="caption" color="muted">
                {error}
              </Text>
            </Alert>
          </div>
        )}

        {/* Footer */}
        {/* eslint-disable-next-line local/no-raw-div */}
        <div className="flex items-center justify-between border-t border-border px-5 py-4">
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
        </div>
      </div>
    </>
  );
}
