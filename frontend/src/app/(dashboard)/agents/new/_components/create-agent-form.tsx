'use client';

/** @tier 3 — agents/new-scoped. Form to create a new agent type. */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Form,
  FormField,
  Heading,
  Icon,
  Input,
  NativeSelect,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import { ScopePicker } from '@orbit/components';
import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  ModelObject,
  ToolScopeDefinition,
  ConnectivityMode,
  Connector,
  ConnectionCatalogEntry,
} from '@orbit/shared/api';
import { createAgentType } from '../../_services/agents-api';
import { ConnectivityFields, textToHosts } from '../../_components/connectivity-fields';
import { ConnectorsFields } from '../../_components/connectors-fields';
import { McpToolsFields } from '../../_components/mcp-tools-fields';
import type { McpCatalogTool, McpServer } from '@orbit/shared/api';
import { EnvironmentField } from '../../_components/environment-field';

// ── Framework options ─────────────────────────────────────────────────────────

const FRAMEWORK_OPTIONS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
] as const;

// ── Scope derivation ──────────────────────────────────────────────────────────

// ── Slug derivation ───────────────────────────────────────────────────────────

function deriveSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

// ── Model grouping ────────────────────────────────────────────────────────────

interface ModelGroup {
  family: string;
  models: ModelObject[];
}

function groupModelsByFamily(models: ModelObject[]): ModelGroup[] {
  const map = new Map<string, ModelObject[]>();
  for (const m of models) {
    const key = m.family || m.owned_by || 'other';
    if (!map.has(key)) map.set(key, []);
    map.get(key)!.push(m);
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([family, ms]) => ({ family, models: ms }));
}

// ── Component ─────────────────────────────────────────────────────────────────

export function CreateAgentForm() {
  const router = useRouter();

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugEdited, setSlugEdited] = useState(false);
  const [description, setDescription] = useState('');
  const [framework, setFramework] = useState<'claude-code' | 'codex' | 'opencode'>('claude-code');
  const [modelId, setModelId] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [maxConcurrentRuns, setMaxConcurrentRuns] = useState('10');
  const [budgetLimitUSD, setBudgetLimitUSD] = useState('');
  const [budgetCadence, setBudgetCadence] = useState('monthly');
  const [budgetPerRunUSD, setBudgetPerRunUSD] = useState('');
  const [connectivityMode, setConnectivityMode] = useState<ConnectivityMode>('none');
  const [connectivityHosts, setConnectivityHosts] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<string, string>>>({});

  const [models, setModels] = useState<ModelObject[]>([]);
  const [modelsLoading, setModelsLoading] = useState(true);
  const [toolScopes, setToolScopes] = useState<string[]>([]);
  const [toolScopesSearch, setToolScopesSearch] = useState('');
  const [availableToolScopes, setAvailableToolScopes] = useState<ToolScopeDefinition[]>([]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [availableConnections, setAvailableConnections] = useState<ConnectionCatalogEntry[]>([]);
  const [mcpServers, setMcpServers] = useState<McpServer[]>([]);
  const [mcpCatalog, setMcpCatalog] = useState<McpCatalogTool[]>([]);
  const [mcpGrantScopes, setMcpGrantScopes] = useState<string[]>([]);

  useEffect(() => {
    fetch(ApiRoute.models.GET(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ModelObject[] }) => {
        const available = (body.data ?? []).filter((m) => m.status === 'available');
        setModels(available);
        if (available.length > 0) setModelId(available[0].id);
      })
      .catch(() => {
        /* silently leave empty — user sees disabled select */
      })
      .finally(() => setModelsLoading(false));
  }, []);

  useEffect(() => {
    fetch(ApiRoute.agentTypes.toolScopes(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ToolScopeDefinition[] }) => {
        setAvailableToolScopes(body.data ?? []);
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    fetch(ApiRoute.agentTypes.connectors(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ConnectionCatalogEntry[] }) => {
        setAvailableConnections(body.data ?? []);
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    fetch(ApiRoute.agentTypes.mcpTools(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { servers?: McpServer[]; catalog?: McpCatalogTool[] }) => {
        setMcpServers(body.servers ?? []);
        setMcpCatalog(body.catalog ?? []);
      })
      .catch(() => undefined);
  }, []);

  const modelGroups = useMemo(() => groupModelsByFamily(models), [models]);

  // Auto-derive slug from name unless user has manually edited it.
  const handleNameChange = useCallback(
    (value: string) => {
      setName(value);
      if (!slugEdited) {
        setSlug(deriveSlug(value));
      }
    },
    [slugEdited],
  );

  const handleSlugChange = useCallback((value: string) => {
    setSlug(value);
    setSlugEdited(true);
  }, []);

  const validate = useCallback((): boolean => {
    const errors: Partial<Record<string, string>> = {};
    if (!name.trim()) errors.name = 'Name is required.';
    if (!slug.trim()) errors.slug = 'Slug is required.';
    else if (!/^[a-z0-9-]+$/.test(slug))
      errors.slug = 'Slug may only contain lowercase letters, numbers, and hyphens.';
    if (!modelId) errors.modelId = 'Select a model for this agent.';
    if (!systemPrompt.trim()) errors.systemPrompt = 'System prompt is required.';
    const maxRuns = parseInt(maxConcurrentRuns, 10);
    if (isNaN(maxRuns) || maxRuns < 1 || maxRuns > 50)
      errors.maxConcurrentRuns = 'Must be between 1 and 50.';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }, [name, slug, modelId, systemPrompt, maxConcurrentRuns]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setError(null);
      if (!validate()) return;

      setSubmitting(true);
      try {
        const result = await createAgentType({
          name: name.trim(),
          slug: slug.trim(),
          description: description.trim(),
          framework,
          model_id: modelId,
          system_prompt: systemPrompt.trim(),
          tool_scopes: toolScopes,
          connectivity:
            connectivityMode === 'whitelist'
              ? { mode: 'whitelist', hosts: textToHosts(connectivityHosts) }
              : { mode: connectivityMode },
          connectors,
          ...(mcpGrantScopes.length > 0 ? { mcp_tools: mcpGrantScopes } : {}),
          ...(environmentId ? { environment_id: environmentId } : {}),
          max_concurrent_runs_per_pod: parseInt(maxConcurrentRuns, 10),
          budget:
            budgetLimitUSD || budgetPerRunUSD
              ? {
                  reset_cadence: budgetCadence,
                  ...(budgetLimitUSD ? { limit_usd: parseFloat(budgetLimitUSD) } : {}),
                  ...(budgetPerRunUSD ? { per_run_limit_usd: parseFloat(budgetPerRunUSD) } : {}),
                }
              : undefined,
        });
        if (result.error) {
          setError(result.error);
          return;
        }
        if (result.agentType) {
          router.push(APP_ROUTES.agents.detail(result.agentType.slug));
        }
      } catch {
        setError('Something went wrong. Please try again.');
      } finally {
        setSubmitting(false);
      }
    },
    [
      validate,
      name,
      slug,
      description,
      framework,
      budgetLimitUSD,
      budgetCadence,
      budgetPerRunUSD,
      modelId,
      systemPrompt,
      toolScopes,
      connectivityMode,
      connectivityHosts,
      connectors,
      mcpGrantScopes,
      environmentId,
      maxConcurrentRuns,
      router,
    ],
  );

  return (
    <Form onSubmit={(e) => void handleSubmit(e)} noValidate gap="comfortable" maxWidth="wide">
      {/* Breadcrumb + heading */}
      <Stack gap="snug">
        <Stack direction="horizontal" gap="snug" align="center">
          <Button variant="ghost" size="sm" asChild>
            <Link href={APP_ROUTES.agents.list}>
              <Icon name="arrow-left" size="xs" />
              Agents
            </Link>
          </Button>
        </Stack>
        <Heading variant="title">New Agent</Heading>
      </Stack>

      {/* Identity section */}
      <Card>
        <CardHeader
          leadingIcon="user"
          title="Identity"
          description="Name, slug, and description for this agent."
          divider
        />
        <CardContent padded>
          <Stack gap="snug">
            <FormField id="agent-name" label="Name" error={fieldErrors.name}>
              <Input
                id="agent-name"
                value={name}
                onChange={(e) => handleNameChange(e.target.value)}
                placeholder="e.g. Security Reviewer"
                maxLength={128}
              />
            </FormField>
            <FormField
              id="agent-slug"
              label="Slug"
              hint="URL-safe identifier, auto-derived from name. Only lowercase letters, numbers, and hyphens."
              error={fieldErrors.slug}
            >
              <Input
                id="agent-slug"
                value={slug}
                onChange={(e) => handleSlugChange(e.target.value)}
                placeholder="e.g. security-reviewer"
                mono
              />
            </FormField>
            <FormField
              id="agent-description"
              label="Description"
              hint="Optional summary of what this agent does."
            >
              <Textarea
                id="agent-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. Reviews code for security vulnerabilities and compliance issues."
                rows={3}
              />
            </FormField>
          </Stack>
        </CardContent>
      </Card>

      {/* Behaviour section */}
      <Card>
        <CardHeader
          leadingIcon="settings"
          title="Behaviour"
          description="Framework, model, and system prompt."
          divider
        />
        <CardContent padded>
          <Stack gap="snug">
            <FormField id="agent-framework" label="Framework">
              <NativeSelect
                id="agent-framework"
                value={framework}
                onChange={(e) => setFramework(e.target.value as typeof framework)}
              >
                {FRAMEWORK_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </NativeSelect>
            </FormField>

            <FormField
              id="agent-model"
              label="Model"
              hint={
                modelId
                  ? 'Inference access for this model is granted automatically.'
                  : 'Select a model — inference access is granted automatically.'
              }
              error={fieldErrors.modelId}
            >
              <NativeSelect
                id="agent-model"
                value={modelId}
                onChange={(e) => setModelId(e.target.value)}
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

            <FormField
              id="agent-system-prompt"
              label="System Prompt"
              hint="Instructions that define the agent's role and behaviour."
              error={fieldErrors.systemPrompt}
            >
              <Textarea
                id="agent-system-prompt"
                value={systemPrompt}
                onChange={(e) => setSystemPrompt(e.target.value)}
                placeholder="You are a security-focused code reviewer. Your role is to…"
                rows={8}
              />
            </FormField>
          </Stack>
        </CardContent>
      </Card>

      {/* Tools section */}
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
            onToggle={(scope, checked) =>
              setToolScopes((prev) =>
                checked ? [...prev, scope] : prev.filter((s) => s !== scope),
              )
            }
            onToggleGroup={(scopes, checked) =>
              setToolScopes((prev) => {
                const without = prev.filter((s) => !scopes.includes(s));
                return checked ? [...without, ...scopes] : without;
              })
            }
            search={toolScopesSearch}
            onSearchChange={setToolScopesSearch}
          />
        </CardContent>
      </Card>

      {/* Connectivity section */}
      <Card>
        <CardHeader
          leadingIcon="network"
          title="Connectivity"
          description="Where this agent's sandboxes may reach on the internet. Hosts needing credentials are configured separately as connections."
          divider
        />
        <CardContent padded>
          <ConnectivityFields
            mode={connectivityMode}
            hosts={connectivityHosts}
            onModeChange={setConnectivityMode}
            onHostsChange={setConnectivityHosts}
          />
        </CardContent>
      </Card>

      {/* Connectors section */}
      <Card>
        <CardHeader
          leadingIcon="key"
          title="Connectors"
          description="Registered connections this agent may use. The gateway injects a scoped, short-lived credential per call — the agent never holds the secret."
          divider
        />
        <CardContent padded>
          <Stack gap="comfortable">
            <ConnectorsFields
              available={availableConnections}
              value={connectors}
              onChange={setConnectors}
            />
            <EnvironmentField value={environmentId} onChange={setEnvironmentId} />
          </Stack>
        </CardContent>
      </Card>

      {/* Governed MCP tools section */}
      <Card>
        <CardHeader
          leadingIcon="server"
          title="MCP tools"
          description="The exact tools this agent may call through the governed MCP gateway. Everything outside this list is filtered from tools/list and rejected at call time."
          divider
        />
        <CardContent padded>
          <McpToolsFields
            servers={mcpServers}
            catalog={mcpCatalog}
            value={mcpGrantScopes}
            onChange={setMcpGrantScopes}
          />
        </CardContent>
      </Card>

      {/* Capacity section */}
      <Card>
        <CardHeader
          leadingIcon="layers"
          title="Capacity"
          description="Concurrency limits for this agent type."
          divider
        />
        <CardContent padded>
          <FormField
            id="agent-max-concurrent"
            label="Max concurrent runs per pod"
            hint="How many simultaneous runs this agent type may have on a single sandbox pod (1–50)."
            error={fieldErrors.maxConcurrentRuns}
          >
            <Input
              id="agent-max-concurrent"
              type="number"
              min={1}
              max={50}
              value={maxConcurrentRuns}
              onChange={(e) => setMaxConcurrentRuns(e.target.value)}
            />
          </FormField>
        </CardContent>
      </Card>

      {/* Budget section — applied to the agent's policy grant. */}
      <Card>
        <CardHeader
          leadingIcon="dollar-sign"
          title="Budget"
          description="Spending ceilings enforced by the inference gateway. The agent is blocked when any ceiling is met; per-run limits reset with every new run."
          divider
        />
        <CardContent padded>
          <Stack gap="comfortable">
            <FormField
              id="agent-budget-limit"
              label="Window limit"
              hint="Maximum spend per week or month across all of this agent's runs. Leave empty for no window limit."
            >
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="muted">
                  $
                </Text>
                <Input
                  id="agent-budget-limit"
                  type="number"
                  min={0}
                  placeholder="e.g. 100"
                  value={budgetLimitUSD}
                  onChange={(e) => setBudgetLimitUSD(e.target.value)}
                />
                <Stack minWidth="form-field-sm">
                  <NativeSelect
                    value={budgetCadence}
                    onChange={(e) => setBudgetCadence(e.target.value)}
                    aria-label="Budget reset window"
                  >
                    <option value="monthly">per month</option>
                    <option value="weekly">per week</option>
                  </NativeSelect>
                </Stack>
              </Stack>
            </FormField>
            <FormField
              id="agent-budget-per-run"
              label="Per-run limit"
              hint="Maximum spend in a single run; every new run starts fresh. Leave empty for no per-run limit."
            >
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="muted">
                  $
                </Text>
                <Input
                  id="agent-budget-per-run"
                  type="number"
                  min={0}
                  placeholder="e.g. 5"
                  value={budgetPerRunUSD}
                  onChange={(e) => setBudgetPerRunUSD(e.target.value)}
                />
                <Text size="caption" color="muted">
                  / run
                </Text>
              </Stack>
            </FormField>
          </Stack>
        </CardContent>
      </Card>

      {error && <Alert variant="error">{error}</Alert>}

      <Stack direction="horizontal" justify="end" gap="snug">
        <Button type="button" variant="outline" size="sm" asChild>
          <Link href={APP_ROUTES.agents.list}>Cancel</Link>
        </Button>
        <Button type="submit" variant="default" size="sm" disabled={submitting}>
          {submitting ? 'Creating…' : 'Create Agent'}
        </Button>
      </Stack>
    </Form>
  );
}
