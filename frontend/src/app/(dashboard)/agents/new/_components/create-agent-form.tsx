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
  Textarea,
} from '@orbit/terra';
import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { ModelObject } from '@orbit/shared/api';
import { createAgentType } from '../../_services/agents-api';

// ── Framework options ─────────────────────────────────────────────────────────

const FRAMEWORK_OPTIONS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
] as const;

// ── Scope derivation ──────────────────────────────────────────────────────────

// Always grant inference:invoke so the agent can reach the endpoint.
// When a specific model is chosen also grant the per-model scope so OPA
// allows that model but nothing else.
function deriveGrantScopes(modelId: string): string[] {
  const scopes: string[] = ['inference:invoke'];
  const parts = modelId.split('/');
  if (parts.length === 2 && parts[0] && parts[1]) {
    scopes.push(`inference:${parts[0]}:${parts[1]}:use`);
  }
  return scopes;
}

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
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<string, string>>>({});

  const [models, setModels] = useState<ModelObject[]>([]);
  const [modelsLoading, setModelsLoading] = useState(true);

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

  const modelGroups = useMemo(() => groupModelsByFamily(models), [models]);

  // Derived grant scopes — recomputed whenever modelId changes.
  const grantScopes = useMemo(() => deriveGrantScopes(modelId), [modelId]);

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
          tool_scopes: [],
          grant_scopes: grantScopes,
          max_concurrent_runs_per_pod: parseInt(maxConcurrentRuns, 10),
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
      modelId,
      systemPrompt,
      grantScopes,
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
                  ? `Grants: ${grantScopes.join(', ')}`
                  : 'Select a model — access scopes are derived automatically.'
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
