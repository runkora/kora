'use client';

import { useState, useEffect } from 'react';
import {
  Alert,
  Badge,
  Button,
  Field,
  Grid,
  Heading,
  Input,
  Label,
  NativeSelect,
  Output,
  Separator,
  SidePanel,
  SidePanelBody,
  SidePanelFooter,
  Stack,
  Text,
  Toggle,
  Icon,
  ConfirmDialog,
} from '@runkora/hades';
import type { VirtualModel, ProviderModel, Provider } from '../_types/catalog';
import type { VirtualModel as ApiVirtualModel } from '../../_services/catalog-server';
import { vmFullId, vmPolicyScope, vmToApiPayload, toVirtualModel } from '../_utils/catalog-api';
import { RoutesTab } from './routes-tab';
import { useHasScope } from '../../../_components/permissions-provider';
import { declarativeRefusal, writeErrorMessage } from '@runkora/lib/declarative';
import { ManagedInGitBadge, PermissionHint } from '@runkora/components';

interface EditorPanelProps {
  model: VirtualModel | null;
  onClose: () => void;
  onSaved: (vm: VirtualModel) => void;
  onDeleted: (id: string) => void;
  /** False when the platform takes virtual models from the configuration repository only. */
  apiAuthoring?: boolean;
  providerModels: ProviderModel[];
  providers: Provider[];
}

/** The virtual model switch is platform-wide, so its refusal names the platform, not the tenant. */
const PLATFORM_LOCKED_MESSAGE =
  'Virtual models on this platform are managed in git. Make changes in the configuration repository.';

function vmWriteError(status: number, body: unknown): string {
  return (body as { code?: unknown } | null)?.code === 'api_authoring_disabled'
    ? PLATFORM_LOCKED_MESSAGE
    : writeErrorMessage(status, body);
}

export function EditorPanel({
  model,
  onClose,
  onSaved,
  onDeleted,
  providerModels,
  providers,
  apiAuthoring = true,
}: EditorPanelProps) {
  const canWrite = useHasScope('platform:catalog:write');
  const [draft, setDraft] = useState<VirtualModel | null>(model);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

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
  // A declared model, or any model with the platform switch off, changes only in the configuration repository.
  const managedInGit = draft.declared === true || !apiAuthoring;
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
        setError(vmWriteError(res.status, await res.json().catch(() => ({}))));
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

  async function handleDelete() {
    if (!draft) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      const res = await fetch(`/api/catalog/virtual-models/${encodeURIComponent(draft.id)}`, {
        method: 'DELETE',
      });
      if (res.status === 204 || res.ok) {
        onDeleted(draft.id);
        onClose();
        return;
      }
      setDeleteError(vmWriteError(res.status, await res.json().catch(() => ({}))));
    } catch {
      setDeleteError('Network error');
    } finally {
      setDeleting(false);
    }
  }

  return (
    <SidePanel open size="md" onClose={onClose}>
      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        paddingBlock="comfortable"
        paddingInline="comfortable"
      >
        <Stack gap="tight">
          <Stack direction="horizontal" align="center" gap="snug">
            <Heading variant="section">{isNew ? 'New Virtual Model' : draft.displayName}</Heading>
            {!isNew && (
              <Badge variant={draft.enabled ? 'healthy' : 'muted'} shape="pill" size="sm">
                {draft.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            )}
            {draft.declared && <ManagedInGitBadge />}
          </Stack>
          {fullId && (
            <Text size="caption" color="muted" mono>
              {fullId}
            </Text>
          )}
        </Stack>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close panel">
          <Icon name="close" />
        </Button>
      </Stack>
      <Separator />

      <SidePanelBody>
        <Stack gap="comfortable">
          {managedInGit && (
            <Alert variant="info">
              <Text size="caption">
                {draft.declared
                  ? declarativeRefusal('managed_by_resource')
                  : PLATFORM_LOCKED_MESSAGE}
              </Text>
            </Alert>
          )}
          {/* Identity fields */}
          <Stack paddingBottom="comfortable">
            <Grid cols={2} gap="snug">
              <Field>
                <Label>Family</Label>
                {knownFamilies.length > 0 ? (
                  <NativeSelect
                    value={draft.family}
                    onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                    mono
                  >
                    <option value="">Select…</option>
                    {knownFamilies.map((f) => (
                      <option key={f} value={f}>
                        {f}
                      </option>
                    ))}
                  </NativeSelect>
                ) : (
                  <Input
                    mono
                    value={draft.family}
                    onChange={(e) => setDraft({ ...draft, family: e.target.value })}
                    placeholder="e.g. anthropic"
                  />
                )}
              </Field>

              <Field>
                <Label>Model name</Label>
                <Input
                  mono
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  placeholder="e.g. claude-sonnet-4-6"
                />
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
                <Output field>
                  <Text size="caption" color="muted" mono>
                    {fullId ? vmPolicyScope(draft) : '—'}
                  </Text>
                </Output>
              </Field>
            </Grid>

            {/* Description spans full width — outside the 2-col grid */}
            <Field>
              <Label>Description</Label>
              <Input
                value={draft.description}
                onChange={(e) => setDraft({ ...draft, description: e.target.value })}
                placeholder="Optional description"
              />
            </Field>
          </Stack>
          <Separator />

          {/* Routes content */}
          <RoutesTab
            model={draft}
            onChange={setDraft}
            providerModels={providerModels}
            providers={providers}
          />
        </Stack>
      </SidePanelBody>

      {error && (
        <Stack paddingInline="comfortable" paddingBlock="snug">
          <Alert variant="error">
            <Icon name="alert-circle" size="2xs" color="status-error" />
            <Text size="caption" color="muted">
              {error}
            </Text>
          </Alert>
        </Stack>
      )}

      <SidePanelFooter justify="between">
        <Stack direction="horizontal" align="center" gap="snug">
          {!isNew && !managedInGit && (
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setDeleteOpen(true)}
                disabled={!canWrite || saving || deleting}
              >
                <Icon name="trash" size="2xs" color="subtle" />
                Delete
              </Button>
            </PermissionHint>
          )}
          <PermissionHint allowed={canWrite} action="change the model catalog">
            <Toggle
              checked={draft.enabled}
              onChange={(v) => setDraft({ ...draft, enabled: v })}
              disabled={!canWrite || managedInGit}
            />
          </PermissionHint>
          <Text size="supporting" color={draft.enabled ? 'foreground' : 'muted'}>
            {draft.enabled ? 'Enabled' : 'Disabled'}
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="snug">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          {!managedInGit && (
            <PermissionHint allowed={canWrite} action="change the model catalog">
              <Button onClick={() => void handleSave()} disabled={!canWrite || saving}>
                {saving ? 'Saving…' : 'Save Changes'}
              </Button>
            </PermissionHint>
          )}
        </Stack>
      </SidePanelFooter>

      <ConfirmDialog
        open={deleteOpen}
        title="Delete Virtual Model"
        description={`Remove "${draft.displayName || draft.name}" and all its routes? This cannot be undone.`}
        confirmLabel="Delete"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void handleDelete()}
        onClose={() => {
          setDeleteOpen(false);
          setDeleteError(null);
        }}
      />
    </SidePanel>
  );
}
