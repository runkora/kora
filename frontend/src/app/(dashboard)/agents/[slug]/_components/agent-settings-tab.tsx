'use client';

/** @tier 3 — agents/[slug]-scoped. Settings — flat section card layout. */

import { useState } from 'react';
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  Form,
  FormField,
  Grid,
  Icon,
  Input,
  NativeSelect,
  Stack,
  Text,
  Textarea,
  Toggle,
  ToggleRow,
} from '@orbit/terra';
import { ConfirmDialog, ScopePicker } from '@orbit/components';
import { useAgentSettings } from '../_hooks/use-agent-settings';

// ── Data ───────────────────────────────────────────────────────────────────────

const FRAMEWORK_OPTIONS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
] as const;

function DangerActionRow({
  label,
  description,
  children,
  highlight,
}: {
  label: string;
  description: string;
  children: React.ReactNode;
  highlight?: boolean;
}) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="comfortable"
      style={{
        padding: '12px 14px',
        border: `1px solid ${highlight ? 'oklch(38% 0.12 25 / 0.5)' : 'oklch(var(--color-border) / 1)'}`,
        borderRadius: '6px',
        background: highlight ? 'oklch(18% 0.06 25 / 0.12)' : undefined,
      }}
    >
      <Stack gap="tight" style={{ flex: 1 }}>
        <Text
          size="supporting"
          weight="emphasis"
          style={highlight ? { color: 'oklch(var(--color-error) / 1)' } : undefined}
        >
          {label}
        </Text>
        <Text size="supporting" color="muted">
          {description}
        </Text>
      </Stack>
      {children}
    </Stack>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AgentSettingsTab() {
  const {
    agent,
    name,
    setName,
    description,
    setDescription,
    framework,
    setFramework,
    systemPrompt,
    setSystemPrompt,
    maxConcurrentRuns,
    setMaxConcurrentRuns,
    modelId,
    setModelId,
    toolScopes,
    setToolScopes,
    toolScopesSearch,
    setToolScopesSearch,
    persistMemory,
    setPersistMemory,
    models,
    modelsLoading,
    modelGroups,
    availableToolScopes,
    grantScopes,
    isDirty,
    saving,
    saveError,
    saveSuccess,
    setSaveSuccess,
    handleSave,
    handleDiscard,
    deleteOpen,
    setDeleteOpen,
    deleting,
    deleteError,
    handleDelete,
  } = useAgentSettings();

  // ── Webhook URLs (UI-only) ─────────────────────────────────────────────────
  const [webhookComplete, setWebhookComplete] = useState('');
  const [webhookError, setWebhookError] = useState('');

  if (!agent) return null;

  const estimatedTokens = Math.round(systemPrompt.split(/\s+/).filter(Boolean).length * 1.33);

  // ── Render ─────────────────────────────────────────────────────────────────

  return (
    <Stack gap="comfortable" paddingBlock="comfortable">
      <Form id="settings-form" onSubmit={(e) => void handleSave(e)} noValidate gap="comfortable">
        {/* ── Identity ──────────────────────────────────────────────────────── */}
        <Card>
          <CardHeader
            leadingIcon="user"
            title="Identity"
            description="How this agent appears in the dashboard and to your team."
            divider
          />
          <CardContent padded>
            <Stack gap="comfortable">
              <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
                <FormField id="settings-name" label="Name">
                  <Input
                    id="settings-name"
                    value={name}
                    onChange={(e) => {
                      setName(e.target.value);
                      setSaveSuccess(false);
                    }}
                  />
                </FormField>
                <FormField id="settings-slug" label="Slug" hint="Cannot be changed after creation.">
                  <Input id="settings-slug" value={agent.slug} disabled mono />
                </FormField>
              </Grid>
              <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
                <FormField id="settings-description" label="Description">
                  <Textarea
                    id="settings-description"
                    value={description}
                    onChange={(e) => {
                      setDescription(e.target.value);
                      setSaveSuccess(false);
                    }}
                    rows={2}
                    placeholder="What does this agent do?"
                  />
                </FormField>
                <FormField id="settings-framework" label="Framework">
                  <NativeSelect
                    id="settings-framework"
                    value={framework}
                    onChange={(e) => {
                      setFramework(e.target.value);
                      setSaveSuccess(false);
                    }}
                  >
                    {FRAMEWORK_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </NativeSelect>
                </FormField>
              </Grid>
            </Stack>
          </CardContent>
        </Card>

        {/* ── Model ─────────────────────────────────────────────────────────── */}
        <Card>
          <CardHeader
            leadingIcon="cpu"
            title="Model"
            description="The AI model powering this agent."
            divider
          />
          <CardContent padded>
            <FormField
              id="settings-model"
              label="Model"
              hint={modelId ? `Grants: ${grantScopes.join(', ')}` : undefined}
            >
              <NativeSelect
                id="settings-model"
                value={modelId}
                onChange={(e) => {
                  setModelId(e.target.value);
                  setSaveSuccess(false);
                }}
                disabled={modelsLoading || models.length === 0}
              >
                {modelsLoading && <option value="">Loading models…</option>}
                {!modelsLoading && models.length === 0 && (
                  <option value="">No models available</option>
                )}
                {!modelsLoading &&
                  modelGroups.map((group) => (
                    <optgroup key={group.family} label={group.family}>
                      {group.models.map((m) => (
                        <option key={m.id} value={m.id}>
                          {m.display_name || m.id}
                        </option>
                      ))}
                    </optgroup>
                  ))}
              </NativeSelect>
            </FormField>
          </CardContent>
        </Card>

        {/* ── System Prompt ─────────────────────────────────────────────────── */}
        <Card>
          <CardHeader
            leadingIcon="file-text"
            title="System Prompt"
            description="Instructions given at the start of every session."
            action={
              estimatedTokens > 0 ? (
                <Text mono size="supporting" color="muted">
                  ~{estimatedTokens.toLocaleString()} tok
                </Text>
              ) : undefined
            }
            divider
          />
          <CardContent padded>
            <Textarea
              id="settings-system-prompt"
              value={systemPrompt}
              onChange={(e) => {
                setSystemPrompt(e.target.value);
                setSaveSuccess(false);
              }}
              rows={12}
            />
          </CardContent>
        </Card>

        {/* ── Tools ─────────────────────────────────────────────────────────── */}
        <Card>
          <CardHeader
            leadingIcon="wrench"
            title="Tools"
            description="Select which tool capabilities this agent is allowed to use."
            divider
          />
          <CardContent padded>
            <ScopePicker
              availableScopes={availableToolScopes.map((s) => s.scope)}
              selectedScopes={toolScopes}
              onToggle={(scope, checked) => {
                setToolScopes((prev) =>
                  checked ? [...prev, scope] : prev.filter((s) => s !== scope),
                );
                setSaveSuccess(false);
              }}
              onToggleGroup={(scopes, checked) => {
                setToolScopes((prev) => {
                  const without = prev.filter((s) => !scopes.includes(s));
                  return checked ? [...without, ...scopes] : without;
                });
                setSaveSuccess(false);
              }}
              search={toolScopesSearch}
              onSearchChange={setToolScopesSearch}
            />
          </CardContent>
        </Card>

        {/* ── Context & Memory + Behaviour ──────────────────────────────────── */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          <Card>
            <CardHeader
              leadingIcon="hard-drive"
              title="Context & Memory"
              description="Knowledge retention and session context."
              divider
            />
            <CardContent padded>
              <ToggleRow
                htmlFor="session-memory"
                label="Session memory"
                description="Store a summary of each session and make it available to future sessions."
                control={
                  <Toggle id="session-memory" checked={persistMemory} onChange={setPersistMemory} />
                }
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader
              leadingIcon="settings"
              title="Behaviour"
              description="Guardrails and operational limits."
              divider
            />
            <CardContent padded>
              <FormField
                id="settings-max-concurrent"
                label="Max concurrent runs per pod"
                hint="1–50 · Agent instances scheduled per pod."
              >
                <Input
                  id="settings-max-concurrent"
                  type="number"
                  min={1}
                  max={50}
                  value={maxConcurrentRuns}
                  onChange={(e) => {
                    setMaxConcurrentRuns(e.target.value);
                    setSaveSuccess(false);
                  }}
                  style={{ maxWidth: '8rem' }}
                />
              </FormField>
            </CardContent>
          </Card>
        </Grid>

        {saveError && <Alert variant="error">{saveError}</Alert>}
      </Form>

      {/* ── MCP Servers + API & Webhooks ────────────────────────────────────── */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        <Card>
          <CardHeader
            leadingIcon="server"
            title="MCP Servers"
            description="Tool servers this agent can connect to."
            action={
              <Button variant="ghost" size="sm">
                Manage →
              </Button>
            }
            divider
          />
          <CardContent padded>
            <Stack gap="snug">
              {[
                {
                  name: 'filesystem',
                  desc: 'Local file system access · 5 tools',
                  connected: true,
                  enabled: true,
                },
                {
                  name: 'github',
                  desc: 'GitHub PRs, issues, and repo management · 12 tools',
                  connected: true,
                  enabled: false,
                },
                {
                  name: 'linear',
                  desc: 'Linear issue management · 8 tools',
                  connected: true,
                  enabled: false,
                },
              ].map((server) => (
                <Stack
                  key={server.name}
                  direction="horizontal"
                  align="center"
                  gap="comfortable"
                  style={{
                    padding: '10px 12px',
                    border: '1px solid oklch(var(--color-border) / 1)',
                    borderRadius: '6px',
                  }}
                >
                  <Stack
                    style={{
                      width: '8px',
                      height: '8px',
                      borderRadius: '50%',
                      flexShrink: 0,
                      background: server.connected
                        ? 'oklch(70% 0.12 140)'
                        : 'oklch(var(--color-border) / 1)',
                      boxShadow: server.connected ? '0 0 6px oklch(70% 0.2 140 / 0.5)' : undefined,
                    }}
                  />
                  <Stack gap="none" style={{ flex: 1 }}>
                    <Text mono size="supporting" color="muted">
                      {server.name}
                    </Text>
                    <Text size="supporting" color="subtle">
                      {server.desc}
                    </Text>
                  </Stack>
                  <Toggle checked={server.enabled} onChange={() => {}} />
                </Stack>
              ))}
            </Stack>
          </CardContent>
        </Card>

        <Card>
          <CardHeader
            leadingIcon="code-2"
            title="API & Webhooks"
            description="Trigger runs programmatically or receive completion events."
            divider
          />
          <CardContent padded>
            <Stack gap="comfortable">
              <Stack gap="snug">
                <Text size="supporting" weight="emphasis">
                  Agent endpoint
                </Text>
                <Card style={{ background: 'oklch(var(--color-background) / 1)' }}>
                  <CardContent padded>
                    <Text
                      mono
                      preserveWhitespace
                      size="supporting"
                      color="muted"
                      style={{ fontSize: '12px', lineHeight: '1.65' }}
                    >
                      {`POST /api/v1/agents/${agent.slug}/runs\nAuthorization: Bearer YOUR_API_KEY\n\n{ "message": "Your task here" }`}
                    </Text>
                  </CardContent>
                </Card>
              </Stack>
              <FormField id="webhook-complete" label="On session complete">
                <Input
                  id="webhook-complete"
                  value={webhookComplete}
                  onChange={(e) => setWebhookComplete(e.target.value)}
                  placeholder="https://your-app.com/webhooks/orbit"
                  mono
                />
              </FormField>
              <FormField id="webhook-error" label="On session error">
                <Input
                  id="webhook-error"
                  value={webhookError}
                  onChange={(e) => setWebhookError(e.target.value)}
                  placeholder="https://your-app.com/webhooks/orbit-errors"
                  mono
                />
              </FormField>
            </Stack>
          </CardContent>
        </Card>
      </Grid>

      {/* ── Access Control ────────────────────────────────────────────────────── */}
      <Card>
        <CardHeader
          leadingIcon="users"
          title="Access Control"
          description="Who in your team can start sessions with this agent."
          divider
        />
        <CardContent padded>
          <Stack gap="comfortable">
            <Stack gap="snug">
              <Text size="supporting" weight="emphasis">
                Visibility
              </Text>
              <Grid cols={{ base: 1, sm: 3 }} gap="snug">
                {[
                  {
                    label: 'Team',
                    desc: 'All workspace members can see and use this agent',
                    active: true,
                  },
                  {
                    label: 'Specific members',
                    desc: 'Only listed members can access this agent',
                    active: false,
                  },
                  { label: 'Only me', desc: 'Private to your account', active: false },
                ].map((opt) => (
                  <Stack
                    key={opt.label}
                    gap="tight"
                    role="button"
                    tabIndex={0}
                    style={{
                      padding: '10px 12px',
                      border: `1px solid ${opt.active ? 'oklch(var(--color-accent) / 0.3)' : 'oklch(var(--color-border) / 1)'}`,
                      borderRadius: '6px',
                      background: opt.active ? 'oklch(var(--color-accent) / 0.08)' : undefined,
                      cursor: 'pointer',
                    }}
                  >
                    <Text
                      size="supporting"
                      weight="emphasis"
                      style={opt.active ? { color: 'oklch(var(--color-accent) / 1)' } : undefined}
                    >
                      {opt.label}
                    </Text>
                    <Text size="supporting" color="muted">
                      {opt.desc}
                    </Text>
                  </Stack>
                ))}
              </Grid>
            </Stack>
            <Stack gap="snug">
              <Text size="supporting" weight="emphasis">
                Members with access
              </Text>
              {[
                {
                  initials: 'JW',
                  name: agent.name ? 'Justin Wilkin' : 'You',
                  role: 'Owner',
                  you: true,
                },
              ].map((member) => (
                <Stack
                  key={member.initials}
                  direction="horizontal"
                  align="center"
                  gap="comfortable"
                  style={{
                    padding: '7px 10px',
                    border: '1px solid oklch(var(--color-border) / 1)',
                    borderRadius: '5px',
                  }}
                >
                  <Stack
                    align="center"
                    justify="center"
                    style={{
                      width: '26px',
                      height: '26px',
                      borderRadius: '50%',
                      background: 'oklch(var(--color-accent) / 0.1)',
                      border: '1.5px solid oklch(var(--color-accent) / 0.3)',
                      flexShrink: 0,
                    }}
                  >
                    <Text
                      size="supporting"
                      style={{
                        fontSize: '11px',
                        fontWeight: 600,
                        color: 'oklch(var(--color-accent) / 1)',
                      }}
                    >
                      {member.initials}
                    </Text>
                  </Stack>
                  <Stack gap="none" style={{ flex: 1 }}>
                    <Stack direction="horizontal" align="center" gap="tight">
                      <Text size="supporting">{member.name}</Text>
                      {member.you && (
                        <Text
                          size="supporting"
                          style={{ fontSize: '11px', color: 'oklch(var(--color-accent) / 1)' }}
                        >
                          you
                        </Text>
                      )}
                    </Stack>
                  </Stack>
                  <Text size="supporting" color="muted">
                    {member.role}
                  </Text>
                </Stack>
              ))}
              <Button variant="ghost" size="sm" style={{ alignSelf: 'flex-start' }}>
                <Icon name="plus" size="xs" />
                Add member
              </Button>
            </Stack>
          </Stack>
        </CardContent>
      </Card>

      {/* ── Danger Zone ───────────────────────────────────────────────────────── */}
      <Card style={{ borderColor: 'oklch(38% 0.12 25 / 0.5)' }}>
        <CardHeader divider>
          <Stack direction="horizontal" align="center" gap="snug">
            <Stack style={{ color: 'oklch(var(--color-error) / 1)', flexShrink: 0 }}>
              <Icon name="triangle-alert" size="xs" />
            </Stack>
            <Stack gap="none">
              <Text
                size="supporting"
                weight="emphasis"
                style={{ color: 'oklch(var(--color-error) / 1)' }}
              >
                Danger Zone
              </Text>
              <CardDescription>
                These actions are permanent and may affect running sessions.
              </CardDescription>
            </Stack>
          </Stack>
        </CardHeader>
        <CardContent padded>
          <Grid cols={{ base: 1, lg: 2 }} gap="snug">
            <DangerActionRow
              label="Duplicate agent"
              description="Create an identical copy of this agent with all settings preserved."
            >
              <Button variant="ghost" size="sm">
                <Icon name="copy" size="xs" />
                Duplicate
              </Button>
            </DangerActionRow>
            <DangerActionRow
              label="Reset to defaults"
              description="Clear all configuration and return to factory defaults. Sessions and logs are preserved."
            >
              <Button variant="ghost" size="sm">
                <Icon name="refresh" size="xs" />
                Reset
              </Button>
            </DangerActionRow>
            <DangerActionRow
              label="Archive agent"
              description="Remove from the active dashboard. Sessions and history are preserved and can be restored."
              highlight
            >
              <Button variant="ghost" size="sm">
                <Icon name="layers" size="xs" />
                Archive
              </Button>
            </DangerActionRow>
            <DangerActionRow
              label="Delete agent permanently"
              description="Deletes the agent, all configuration, and the complete session history. Cannot be undone."
              highlight
            >
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                <Icon name="trash" size="xs" />
                Delete agent
              </Button>
            </DangerActionRow>
          </Grid>
        </CardContent>
      </Card>

      {/* ── Floating save bar ─────────────────────────────────────────────────── */}
      {(isDirty || saveSuccess) && (
        <Stack
          direction="horizontal"
          align="center"
          gap="comfortable"
          style={{
            position: 'fixed',
            bottom: '20px',
            left: '50%',
            transform: 'translateX(-50%)',
            background: saveSuccess ? 'oklch(25% 0.06 145)' : 'oklch(18% 0 0)',
            border: `1px solid ${saveSuccess ? 'oklch(50% 0.14 145 / 0.6)' : 'oklch(var(--color-border) / 1)'}`,
            borderRadius: '8px',
            padding: '10px 16px',
            boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
            zIndex: 100,
            whiteSpace: 'nowrap' as const,
            transition: 'border-color 0.2s',
          }}
        >
          {saveSuccess ? (
            <Stack direction="horizontal" align="center" gap="snug">
              <Stack style={{ color: 'oklch(65% 0.16 145)' }}>
                <Icon name="check-circle" size="xs" />
              </Stack>
              <Text size="supporting" style={{ color: 'oklch(65% 0.16 145)' }}>
                Changes saved
              </Text>
            </Stack>
          ) : (
            <>
              <Stack direction="horizontal" align="center" gap="snug">
                <Stack style={{ color: 'oklch(80% 0.15 80)' }}>
                  <Icon name="info" size="xs" />
                </Stack>
                <Text size="supporting">You have unsaved changes</Text>
              </Stack>
              <Stack direction="horizontal" gap="snug">
                <Button variant="ghost" size="sm" onClick={handleDiscard}>
                  Discard
                </Button>
                <Button
                  type="submit"
                  form="settings-form"
                  variant="outline"
                  size="sm"
                  disabled={saving}
                >
                  <Icon name="check" size="xs" />
                  {saving ? 'Saving…' : 'Save changes'}
                </Button>
              </Stack>
            </>
          )}
        </Stack>
      )}

      <ConfirmDialog
        open={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        title="Delete agent"
        description={`Permanently delete "${agent.name}"? All associated runs will be terminated. This cannot be undone.`}
        confirmLabel="Delete agent"
        onConfirm={() => void handleDelete()}
        confirming={deleting}
        error={deleteError}
      />
    </Stack>
  );
}
