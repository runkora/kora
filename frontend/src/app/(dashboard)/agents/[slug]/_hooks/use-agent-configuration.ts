'use client';

/** @tier 3 — agents/[slug]-scoped. State for the Configuration tab: every
 * editable facet of the agent behind ONE dirty baseline and ONE save, so an
 * editing session becomes a single reviewable re-provision
 * (docs/adr/2026-07-17-agent-detail-configuration.md §4). Replaces the split
 * use-agent-settings / use-agent-connectivity pair. */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';
import type {
  AgentFramework,
  ConnectionCatalogEntry,
  Connector,
  ConnectivityMode,
  EffectiveAccess,
  Environment,
  McpCatalogTool,
  McpServer,
  ModelObject,
} from '@orbit/shared/api';
import {
  fetchConnections,
  fetchEnvironments,
  fetchMcpCatalogue,
  fetchModels,
} from '../../_services/agents-api';
import { previewEffectiveAccess, updateAgentType } from '../_services/agent-detail-api';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import {
  loopGuardrailErrors,
  loopGuardrailsPayload,
  loopGuardrailValues,
  type AgentTypeRow,
  type LoopGuardrailValues,
} from '../../_types';

// Debounce window for the live effective-access preview as the user edits.
const PREVIEW_DEBOUNCE_MS = 400;

/** Order-independent canonical form for dirty-checking connectors. */
function normalizeConnectors(connectors: Connector[]): string {
  return JSON.stringify(
    connectors
      .map((c) => ({
        connection: c.connection,
        actions: c.actions.slice().sort(),
        // Resource qualifiers participate in dirty-checking; absent and empty
        // both mean "all resources" and normalize identically.
        resources: (c.resources ?? []).slice().sort(),
      }))
      .sort((a, b) => a.connection.localeCompare(b.connection)),
  );
}

const sortedJson = (xs: string[]) => JSON.stringify(xs.slice().sort());

interface ConfigurationBaseline {
  name: string;
  description: string;
  framework: AgentFramework;
  systemPrompt: string;
  modelId: string;
  mcpTools: string[];
  connectors: Connector[];
  connectivityMode: ConnectivityMode;
  connectivityHosts: string;
  environmentId: string;
  maxConcurrentRuns: string;
  loopGuardrails: LoopGuardrailValues;
  budgetLimitUSD: string;
  budgetCadence: string;
  budgetPerRunUSD: string;
}

function baselineFrom(agent: AgentTypeRow | null): ConfigurationBaseline {
  return {
    name: agent?.name ?? '',
    description: agent?.description ?? '',
    framework: (agent?.frameworkKey ?? 'claude-code') as AgentFramework,
    systemPrompt: agent?.systemPrompt ?? '',
    modelId: agent?.modelId ?? '',
    mcpTools: agent?.mcpTools ?? [],
    connectors: agent?.connectors ?? [],
    connectivityMode: agent?.connectivity?.mode ?? 'none',
    connectivityHosts: (agent?.connectivity?.hosts ?? []).join('\n'),
    environmentId: agent?.environmentId ?? '',
    maxConcurrentRuns: String(agent?.maxConcurrentRunsPerPod ?? 10),
    loopGuardrails: loopGuardrailValues(agent?.guardrails),
    budgetLimitUSD: agent?.budget?.limit_usd ? String(agent.budget.limit_usd) : '',
    budgetCadence: agent?.budget?.reset_cadence ?? 'monthly',
    budgetPerRunUSD: agent?.budget?.per_run_limit_usd ? String(agent.budget.per_run_limit_usd) : '',
  };
}

export function useAgentConfiguration() {
  const { agent, deleteAgent } = useAgentDetailContext();
  const router = useRouter();

  const initial = useMemo(() => baselineFrom(agent), [agent]);

  // ── Form state (one field set across every section) ────────────────────────
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [framework, setFramework] = useState<AgentFramework>(initial.framework);
  const [systemPrompt, setSystemPrompt] = useState(initial.systemPrompt);
  const [modelId, setModelId] = useState(initial.modelId);
  const [mcpTools, setMcpTools] = useState<string[]>(initial.mcpTools);
  const [connectors, setConnectors] = useState<Connector[]>(initial.connectors);
  const [connectivityMode, setConnectivityMode] = useState<ConnectivityMode>(
    initial.connectivityMode,
  );
  const [connectivityHosts, setConnectivityHosts] = useState(initial.connectivityHosts);
  const [environmentId, setEnvironmentId] = useState(initial.environmentId);
  const [maxConcurrentRuns, setMaxConcurrentRuns] = useState(initial.maxConcurrentRuns);
  const [loopGuardrails, setLoopGuardrails] = useState<LoopGuardrailValues>(initial.loopGuardrails);
  const setLoopGuardrail = useCallback(
    <K extends keyof LoopGuardrailValues>(key: K, value: LoopGuardrailValues[K]) =>
      setLoopGuardrails((prev) => ({ ...prev, [key]: value })),
    [],
  );
  const [budgetLimitUSD, setBudgetLimitUSD] = useState(initial.budgetLimitUSD);
  const [budgetCadence, setBudgetCadence] = useState(initial.budgetCadence);
  const [budgetPerRunUSD, setBudgetPerRunUSD] = useState(initial.budgetPerRunUSD);

  const [savedBaseline, setSavedBaseline] = useState<ConfigurationBaseline>(initial);

  // ── Catalogues (same no-throw fetchers the builder uses) ────────────────────
  const [models, setModels] = useState<ModelObject[]>([]);
  const [modelsError, setModelsError] = useState<string | null>(null);
  const [availableConnections, setAvailableConnections] = useState<ConnectionCatalogEntry[]>([]);
  const [mcpServers, setMcpServers] = useState<McpServer[]>([]);
  const [mcpCatalog, setMcpCatalog] = useState<McpCatalogTool[]>([]);
  const [mcpError, setMcpError] = useState<string | null>(null);
  const [environments, setEnvironments] = useState<Environment[]>([]);

  useEffect(() => {
    void fetchModels().then(({ data, error }) => {
      setModels(data);
      setModelsError(error);
    });
    void fetchConnections().then(({ data }) => setAvailableConnections(data));
    void fetchMcpCatalogue().then(({ data, error }) => {
      setMcpServers(data.servers);
      setMcpCatalog(data.catalog);
      setMcpError(error);
    });
    void fetchEnvironments().then(({ data }) => setEnvironments(data));
  }, []);

  // ── Effective access (live, draft-driven preview) ───────────────────────────
  const [effectiveAccess, setEffectiveAccess] = useState<EffectiveAccess | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => {
      void previewEffectiveAccess({
        connectivity: {
          mode: connectivityMode,
          hosts: connectivityHosts
            .split('\n')
            .map((h) => h.trim())
            .filter(Boolean),
        },
        connectors,
        environment_id: environmentId || null,
      }).then((result) => {
        setEffectiveAccess(result.access);
        setAccessError(result.error);
      });
    }, PREVIEW_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [connectivityMode, connectivityHosts, connectors, environmentId]);

  // ── Dirty check ─────────────────────────────────────────────────────────────
  const isDirty =
    name !== savedBaseline.name ||
    description !== savedBaseline.description ||
    framework !== savedBaseline.framework ||
    systemPrompt !== savedBaseline.systemPrompt ||
    modelId !== savedBaseline.modelId ||
    sortedJson(mcpTools) !== sortedJson(savedBaseline.mcpTools) ||
    normalizeConnectors(connectors) !== normalizeConnectors(savedBaseline.connectors) ||
    connectivityMode !== savedBaseline.connectivityMode ||
    connectivityHosts !== savedBaseline.connectivityHosts ||
    environmentId !== savedBaseline.environmentId ||
    maxConcurrentRuns !== savedBaseline.maxConcurrentRuns ||
    JSON.stringify(loopGuardrails) !== JSON.stringify(savedBaseline.loopGuardrails) ||
    budgetLimitUSD !== savedBaseline.budgetLimitUSD ||
    budgetCadence !== savedBaseline.budgetCadence ||
    budgetPerRunUSD !== savedBaseline.budgetPerRunUSD;

  // ── Save: one PATCH, one re-provision ───────────────────────────────────────
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const saveSuccessTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleSave = useCallback(async () => {
    if (!agent) return;
    const maxRuns = parseInt(maxConcurrentRuns, 10);
    if (isNaN(maxRuns) || maxRuns < 1 || maxRuns > 50) {
      setSaveSuccess(false);
      setSaveError('Max concurrent runs must be between 1 and 50.');
      return;
    }
    const loopErrors = loopGuardrailErrors(loopGuardrails);
    if (Object.keys(loopErrors).length > 0) {
      setSaveSuccess(false);
      setSaveError(loopErrors.repeatedCall ?? loopErrors.waitRearm ?? null);
      return;
    }
    setSaving(true);
    setSaveError(null);
    setSaveSuccess(false);
    const trimmedName = name.trim();
    const trimmedDescription = description.trim();
    const trimmedSystemPrompt = systemPrompt.trim();
    const hosts = connectivityHosts
      .split('\n')
      .map((h) => h.trim())
      .filter(Boolean);
    // Budget: omit = unchanged; both limits zero = remove (see UpdateAgentTypeRequest).
    const budgetChanged =
      budgetLimitUSD !== savedBaseline.budgetLimitUSD ||
      budgetCadence !== savedBaseline.budgetCadence ||
      budgetPerRunUSD !== savedBaseline.budgetPerRunUSD;
    const result = await updateAgentType(agent.id, {
      name: trimmedName,
      description: trimmedDescription,
      framework,
      system_prompt: trimmedSystemPrompt,
      max_concurrent_runs_per_pod: maxRuns,
      guardrails: loopGuardrailsPayload(loopGuardrails),
      model_id: modelId,
      mcp_tools: mcpTools,
      connectors,
      connectivity: { mode: connectivityMode, hosts },
      environment_id: environmentId,
      ...(budgetChanged
        ? {
            budget: {
              reset_cadence: budgetCadence,
              limit_usd: budgetLimitUSD ? parseFloat(budgetLimitUSD) : 0,
              per_run_limit_usd: budgetPerRunUSD ? parseFloat(budgetPerRunUSD) : 0,
            },
          }
        : {}),
    });
    setSaving(false);
    if (result.error) {
      setSaveError(result.error);
      return;
    }
    setSavedBaseline({
      name: trimmedName,
      description: trimmedDescription,
      framework,
      systemPrompt: trimmedSystemPrompt,
      modelId,
      mcpTools,
      connectors,
      connectivityMode,
      connectivityHosts,
      environmentId,
      maxConcurrentRuns,
      loopGuardrails,
      budgetLimitUSD,
      budgetCadence,
      budgetPerRunUSD,
    });
    setSaveSuccess(true);
    if (saveSuccessTimerRef.current) clearTimeout(saveSuccessTimerRef.current);
    saveSuccessTimerRef.current = setTimeout(() => setSaveSuccess(false), 2000);
  }, [
    agent,
    name,
    description,
    framework,
    systemPrompt,
    modelId,
    mcpTools,
    connectors,
    connectivityMode,
    connectivityHosts,
    environmentId,
    maxConcurrentRuns,
    loopGuardrails,
    budgetLimitUSD,
    budgetCadence,
    budgetPerRunUSD,
    savedBaseline,
  ]);

  const handleDiscard = useCallback(() => {
    setName(savedBaseline.name);
    setDescription(savedBaseline.description);
    setFramework(savedBaseline.framework);
    setSystemPrompt(savedBaseline.systemPrompt);
    setModelId(savedBaseline.modelId);
    setMcpTools(savedBaseline.mcpTools);
    setConnectors(savedBaseline.connectors);
    setConnectivityMode(savedBaseline.connectivityMode);
    setConnectivityHosts(savedBaseline.connectivityHosts);
    setEnvironmentId(savedBaseline.environmentId);
    setMaxConcurrentRuns(savedBaseline.maxConcurrentRuns);
    setLoopGuardrails(savedBaseline.loopGuardrails);
    setBudgetLimitUSD(savedBaseline.budgetLimitUSD);
    setBudgetCadence(savedBaseline.budgetCadence);
    setBudgetPerRunUSD(savedBaseline.budgetPerRunUSD);
    setSaveError(null);
  }, [savedBaseline]);

  // ── Delete (Danger zone section) ────────────────────────────────────────────
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
    // fields
    name,
    setName,
    description,
    setDescription,
    framework,
    setFramework,
    systemPrompt,
    setSystemPrompt,
    modelId,
    setModelId,
    mcpTools,
    setMcpTools,
    connectors,
    setConnectors,
    connectivityMode,
    setConnectivityMode,
    connectivityHosts,
    setConnectivityHosts,
    environmentId,
    setEnvironmentId,
    maxConcurrentRuns,
    setMaxConcurrentRuns,
    loopGuardrails,
    setLoopGuardrail,
    budgetLimitUSD,
    setBudgetLimitUSD,
    budgetCadence,
    setBudgetCadence,
    budgetPerRunUSD,
    setBudgetPerRunUSD,
    // catalogues
    models,
    modelsError,
    availableConnections,
    mcpServers,
    mcpCatalog,
    mcpError,
    environments,
    // effective access
    effectiveAccess,
    accessError,
    // save lifecycle
    isDirty,
    saving,
    saveError,
    saveSuccess,
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
