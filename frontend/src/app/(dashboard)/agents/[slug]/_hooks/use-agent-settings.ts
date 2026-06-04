'use client';

/** @tier 3 — agents/[slug]-scoped. Business logic for the agent settings form. */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { ModelObject, ToolScopeDefinition } from '@orbit/shared/api';
import { updateAgentType } from '../_services/agent-detail-api';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import type { AgentTypeRow } from '../../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

interface ModelGroup {
  family: string;
  models: ModelObject[];
}

export function groupModelsByFamily(models: ModelObject[]): ModelGroup[] {
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

export function deriveGrantScopes(modelId: string): string[] {
  const scopes: string[] = ['inference:invoke'];
  const parts = modelId.split('/');
  if (parts.length === 2 && parts[0] && parts[1]) {
    scopes.push(`inference:${parts[0]}:${parts[1]}:use`);
  }
  return scopes;
}

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useAgentSettings() {
  const { agent, deleteAgent } = useAgentDetailContext();
  const router = useRouter();

  // ── Form state ──────────────────────────────────────────────────────────────
  const [name, setName] = useState(agent?.name ?? '');
  const [description, setDescription] = useState(agent?.description ?? '');
  const [framework, setFramework] = useState(agent?.frameworkKey ?? 'claude-code');
  const [systemPrompt, setSystemPrompt] = useState(agent?.systemPrompt ?? '');
  const [maxConcurrentRuns, setMaxConcurrentRuns] = useState(
    String(agent?.maxConcurrentRunsPerPod ?? 10),
  );
  const [modelId, setModelId] = useState(agent?.modelId ?? 'anthropic/claude-sonnet-4-6');
  const [toolScopes, setToolScopes] = useState<string[]>(agent?.toolScopes ?? []);
  const [toolScopesSearch, setToolScopesSearch] = useState('');
  const [persistMemory, setPersistMemory] = useState(false);

  // Tracks the last-saved values so isDirty resets immediately after a successful save.
  const [savedBaseline, setSavedBaseline] = useState({
    name: agent?.name ?? '',
    description: agent?.description ?? '',
    framework: agent?.frameworkKey ?? 'claude-code',
    systemPrompt: agent?.systemPrompt ?? '',
    maxConcurrentRuns: String(agent?.maxConcurrentRunsPerPod ?? 10),
    modelId: agent?.modelId ?? 'anthropic/claude-sonnet-4-6',
    toolScopes: agent?.toolScopes ?? ([] as string[]),
  });

  // ── Model data ──────────────────────────────────────────────────────────────
  const [models, setModels] = useState<ModelObject[]>([]);
  const [modelsLoading, setModelsLoading] = useState(true);

  useEffect(() => {
    fetch(ApiRoute.models.GET(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ModelObject[] }) => {
        setModels((body.data ?? []).filter((m) => m.status === 'available'));
      })
      .catch(() => undefined)
      .finally(() => setModelsLoading(false));
  }, []);

  const modelGroups = useMemo(() => groupModelsByFamily(models), [models]);
  const grantScopes = useMemo(() => deriveGrantScopes(modelId), [modelId]);

  // ── Tool scope data ─────────────────────────────────────────────────────────
  const [availableToolScopes, setAvailableToolScopes] = useState<ToolScopeDefinition[]>([]);

  useEffect(() => {
    fetch(ApiRoute.agentTypes.toolScopes(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ToolScopeDefinition[] }) => {
        setAvailableToolScopes(body.data ?? []);
      })
      .catch(() => undefined);
  }, []);

  // ── Derived ─────────────────────────────────────────────────────────────────
  const isDirty =
    name !== savedBaseline.name ||
    description !== savedBaseline.description ||
    framework !== savedBaseline.framework ||
    systemPrompt !== savedBaseline.systemPrompt ||
    maxConcurrentRuns !== savedBaseline.maxConcurrentRuns ||
    modelId !== savedBaseline.modelId ||
    JSON.stringify(toolScopes.slice().sort()) !==
      JSON.stringify(savedBaseline.toolScopes.slice().sort());

  // ── Save / status ───────────────────────────────────────────────────────────
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const saveSuccessTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleSave = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!agent) return;
      // Validate before submit (matches the create form's bounds). Without this,
      // an empty/non-numeric field becomes NaN → null on the wire → the backend
      // treats it as "no change" and the edit is silently lost.
      const maxRuns = parseInt(maxConcurrentRuns, 10);
      if (isNaN(maxRuns) || maxRuns < 1 || maxRuns > 50) {
        setSaveSuccess(false);
        setSaveError('Max concurrent runs must be between 1 and 50.');
        return;
      }
      setSaving(true);
      setSaveError(null);
      setSaveSuccess(false);
      const trimmedName = name.trim();
      const trimmedDescription = description.trim();
      const trimmedSystemPrompt = systemPrompt.trim();
      const result = await updateAgentType(agent.id, {
        name: trimmedName,
        description: trimmedDescription,
        framework: framework as 'claude-code' | 'codex' | 'opencode',
        system_prompt: trimmedSystemPrompt,
        max_concurrent_runs_per_pod: maxRuns,
        model_id: modelId,
        grant_scopes: grantScopes,
        tool_scopes: toolScopes,
      });
      setSaving(false);
      if (result.error) {
        setSaveError(result.error);
      } else {
        // Update the baseline so isDirty immediately resets to false.
        setSavedBaseline({
          name: trimmedName,
          description: trimmedDescription,
          framework,
          systemPrompt: trimmedSystemPrompt,
          maxConcurrentRuns,
          modelId,
          toolScopes,
        });
        setSaveSuccess(true);
        if (saveSuccessTimerRef.current) clearTimeout(saveSuccessTimerRef.current);
        saveSuccessTimerRef.current = setTimeout(() => setSaveSuccess(false), 2000);
      }
    },
    [
      agent,
      name,
      description,
      framework,
      systemPrompt,
      maxConcurrentRuns,
      modelId,
      grantScopes,
      toolScopes,
    ],
  );

  const handleDiscard = useCallback(() => {
    if (!agent) return;
    setName(savedBaseline.name);
    setDescription(savedBaseline.description);
    setFramework(savedBaseline.framework);
    setSystemPrompt(savedBaseline.systemPrompt);
    setMaxConcurrentRuns(savedBaseline.maxConcurrentRuns);
    setModelId(savedBaseline.modelId);
    setToolScopes(savedBaseline.toolScopes);
    setSaveError(null);
  }, [agent, savedBaseline]);

  // ── Delete ──────────────────────────────────────────────────────────────────
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    setDeleteError(null);
    const ok = await deleteAgent();
    setDeleting(false);
    if (!ok) {
      setDeleteError('Failed to delete agent. Please try again.');
      return;
    }
    setDeleteOpen(false);
    router.push(APP_ROUTES.agents.list);
  }, [deleteAgent, router]);

  return {
    agent: agent as AgentTypeRow | null,
    // form fields
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
    // model data
    models,
    modelsLoading,
    modelGroups,
    // tool scope data
    availableToolScopes,
    // derived
    grantScopes,
    isDirty,
    // save status
    saving,
    saveError,
    saveSuccess,
    setSaveSuccess,
    // handlers
    handleSave,
    handleDiscard,
    // delete
    deleteOpen,
    setDeleteOpen,
    deleting,
    deleteError,
    handleDelete,
  };
}
