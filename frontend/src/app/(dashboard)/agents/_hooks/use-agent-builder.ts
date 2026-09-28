'use client';

// FeatureStateShape upgrade contract: when this feature graduates to Zustand,
// the store's state + actions must satisfy the AgentBuilderStateShape interface.
//
// The agent builder's state machine (docs/adr/2026-07-17-agent-builder.md §3):
// owns the draft lifecycle, the five-step navigation, per-step validity,
// per-catalogue load status, and submission. Step components consume this via
// AgentBuilderProvider and never fetch.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import { hasScopeCovering } from '@orbit/lib/permissions';
import { usePermissions } from '../../_components/permissions-provider';
import type {
  AgentFramework,
  AgentIdentityMode,
  AgentTypeDraft,
  ConnectionCatalogEntry,
  Connector,
  ConnectivityMode,
  CreateAgentTypeRequest,
  Environment,
  McpCatalogTool,
  McpServer,
  ModelObject,
  ToolScopeDefinition,
} from '@orbit/shared/api';
import {
  createAgentType,
  createDraft,
  deleteDraft,
  fetchConnections,
  fetchDraft,
  fetchEnvironments,
  fetchMcpCatalogue,
  fetchModels,
  fetchToolScopes,
  updateDraft,
} from '../_services/agents-api';
import type { AgentTypeRow } from '../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';
export type SaveStatus = 'unsaved' | 'saving' | 'saved' | 'error';

export const BUILDER_STEPS = [
  { key: 'identity', title: 'Identity', sub: 'Name & owner', icon: 'user' },
  { key: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
  {
    key: 'tools',
    title: 'Tools & access',
    shortTitle: 'Tools',
    sub: 'Servers & scopes',
    icon: 'wrench',
  },
  { key: 'guardrails', title: 'Guardrails', sub: 'Limits & policy', icon: 'shield' },
  {
    key: 'review',
    title: 'Review & create',
    shortTitle: 'Review',
    sub: 'Confirm & create',
    icon: 'rocket',
  },
] as const;

export interface BuilderFields {
  name: string;
  slug: string;
  slugEdited: boolean;
  description: string;
  /** Immutable after creation: who the agent acts as (autonomous | delegated). */
  identityMode: AgentIdentityMode;
  systemPrompt: string;
  framework: AgentFramework;
  modelId: string;
  mcpTools: string[];
  toolScopes: string[];
  connectors: Connector[];
  connectivityMode: ConnectivityMode;
  /** Textarea form — one host per line (connectivity-fields hostsToText). */
  connectivityHosts: string;
  environmentId: string;
  agentBudgetOn: boolean;
  agentBudgetUsd: string;
  agentBudgetCadence: 'weekly' | 'monthly';
  runBudgetOn: boolean;
  runBudgetUsd: string;
  capacityOn: boolean;
  capacity: string;
  subagentCapOn: boolean;
  subagentCap: string;
  nestedSubagents: boolean;
}

export interface BuilderCatalogues {
  models: ModelObject[];
  toolScopes: ToolScopeDefinition[];
  connections: ConnectionCatalogEntry[];
  mcpServers: McpServer[];
  mcpCatalog: McpCatalogTool[];
  environments: Environment[];
  status: Record<'models' | 'toolScopes' | 'connections' | 'mcp' | 'environments', LoadStatus>;
  errors: Partial<Record<'models' | 'toolScopes' | 'connections' | 'mcp' | 'environments', string>>;
}

/**
 * Mode-qualified agent-type create permissions, resolved from the session's
 * effective scope set with the prefix-covers hierarchy (agents:types:write
 * covers both children). Until `loaded`, both modes read as permitted; the
 * backend's service-layer check is authoritative either way.
 */
export interface IdentityModeAccess {
  loaded: boolean;
  /** May create autonomous agent types (agents:types:autonomous:write). */
  autonomous: boolean;
  /** May create delegated agent types (agents:types:delegated:write). */
  delegated: boolean;
}

export interface AgentBuilderStateShape {
  step: number;
  maxVisited: number;
  fields: BuilderFields;
  catalogues: BuilderCatalogues;
  identityModeAccess: IdentityModeAccess;
  draftId: string | null;
  saveStatus: SaveStatus;
  submitting: boolean;
  submitError: string | null;
  created: AgentTypeRow | null;
  stepValid: boolean;
  stepHint: string | undefined;
  createBlockers: string[];
  setField: <K extends keyof BuilderFields>(key: K, value: BuilderFields[K]) => void;
  setName: (name: string) => void;
  setSlug: (slug: string) => void;
  goTo: (step: number) => void;
  next: () => void;
  back: () => void;
  saveNow: () => void;
  submit: () => Promise<void>;
}

const EMPTY_FIELDS: BuilderFields = {
  name: '',
  slug: '',
  slugEdited: false,
  description: '',
  identityMode: 'autonomous',
  systemPrompt: '',
  framework: 'claude-code',
  modelId: '',
  mcpTools: [],
  toolScopes: [],
  connectors: [],
  connectivityMode: 'none',
  connectivityHosts: '',
  environmentId: '',
  agentBudgetOn: false,
  agentBudgetUsd: '500.00',
  agentBudgetCadence: 'monthly',
  runBudgetOn: false,
  runBudgetUsd: '5.00',
  capacityOn: false,
  capacity: '10',
  subagentCapOn: false,
  subagentCap: '4',
  nestedSubagents: true,
};

/** The draft payload: a partial create request plus builder-only resume state. */
type DraftPayload = Partial<CreateAgentTypeRequest> & { _builder?: { step?: number } };

const SLUG_RE = /^[a-z0-9-]+$/;

export function deriveSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

// ── Payload mapping ───────────────────────────────────────────────────────────

function toPayload(fields: BuilderFields, step: number): DraftPayload {
  const hosts = fields.connectivityHosts
    .split('\n')
    .map((h) => h.trim())
    .filter(Boolean);
  const budget: CreateAgentTypeRequest['budget'] = {};
  if (fields.agentBudgetOn && Number(fields.agentBudgetUsd) > 0) {
    budget.limit_usd = Number(fields.agentBudgetUsd);
    budget.reset_cadence = fields.agentBudgetCadence;
  }
  if (fields.runBudgetOn && Number(fields.runBudgetUsd) > 0) {
    budget.per_run_limit_usd = Number(fields.runBudgetUsd);
  }
  const payload: DraftPayload = {
    name: fields.name,
    slug: fields.slug,
    description: fields.description || undefined,
    identity_mode: fields.identityMode,
    system_prompt: fields.systemPrompt || undefined,
    framework: fields.framework,
    model_id: fields.modelId || undefined,
    mcp_tools: fields.mcpTools,
    tool_scopes: fields.toolScopes,
    connectors: fields.connectors,
    connectivity: { mode: fields.connectivityMode, hosts },
    environment_id: fields.environmentId || undefined,
    _builder: { step },
  };
  if (Object.keys(budget).length > 0) payload.budget = budget;
  if (fields.capacityOn && Number(fields.capacity) > 0) {
    payload.max_concurrent_runs_per_pod = Number(fields.capacity);
  }
  if (fields.subagentCapOn && Number(fields.subagentCap) > 0) {
    payload.max_concurrent_subagents = Number(fields.subagentCap);
  }
  payload.nested_subagents = fields.nestedSubagents;
  return payload;
}

function fromPayload(payload: DraftPayload): { fields: BuilderFields; step: number } {
  const fields: BuilderFields = {
    ...EMPTY_FIELDS,
    name: payload.name ?? '',
    slug: payload.slug ?? '',
    slugEdited: (payload.slug ?? '') !== deriveSlug(payload.name ?? ''),
    description: payload.description ?? '',
    identityMode: payload.identity_mode ?? 'autonomous',
    systemPrompt: payload.system_prompt ?? '',
    framework: payload.framework ?? 'claude-code',
    modelId: payload.model_id ?? '',
    mcpTools: payload.mcp_tools ?? [],
    toolScopes: payload.tool_scopes ?? [],
    connectors: payload.connectors ?? [],
    connectivityMode: payload.connectivity?.mode ?? 'none',
    connectivityHosts: (payload.connectivity?.hosts ?? []).join('\n'),
    environmentId: payload.environment_id ?? '',
    agentBudgetOn: (payload.budget?.limit_usd ?? 0) > 0,
    agentBudgetUsd: payload.budget?.limit_usd ? String(payload.budget.limit_usd) : '500.00',
    agentBudgetCadence: payload.budget?.reset_cadence === 'weekly' ? 'weekly' : 'monthly',
    runBudgetOn: (payload.budget?.per_run_limit_usd ?? 0) > 0,
    runBudgetUsd: payload.budget?.per_run_limit_usd
      ? String(payload.budget.per_run_limit_usd)
      : '5.00',
    capacityOn: (payload.max_concurrent_runs_per_pod ?? 0) > 0,
    capacity: payload.max_concurrent_runs_per_pod
      ? String(payload.max_concurrent_runs_per_pod)
      : '10',
    subagentCapOn: (payload.max_concurrent_subagents ?? 0) > 0,
    subagentCap: payload.max_concurrent_subagents ? String(payload.max_concurrent_subagents) : '4',
    nestedSubagents: payload.nested_subagents ?? true,
  };
  const step = Math.min(Math.max(payload._builder?.step ?? 0, 0), BUILDER_STEPS.length - 1);
  return { fields, step };
}

// ── Hook ──────────────────────────────────────────────────────────────────────

const SAVE_DEBOUNCE_MS = 800;
// The debounce re-arms on every change, so steady activity would postpone the
// save forever. The deadline is a non-resetting cap: the first change arms it,
// and it flushes regardless of further typing.
const SAVE_DEADLINE_MS = 2500;

// The hook lives inside AgentBuilderHost, which is mounted only while the
// builder is open — mount is open, unmount is reset.
export function useAgentBuilder(params: {
  /** null = fresh builder; an id = resume that draft. */
  initialDraftId: string | null;
}): AgentBuilderStateShape {
  const { initialDraftId } = params;

  const [fields, setFields] = useState<BuilderFields>(EMPTY_FIELDS);
  const [step, setStep] = useState(0);
  const [maxVisited, setMaxVisited] = useState(0);
  const [draftId, setDraftId] = useState<string | null>(null);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>('unsaved');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState<AgentTypeRow | null>(null);
  const [catalogues, setCatalogues] = useState<BuilderCatalogues>({
    models: [],
    toolScopes: [],
    connections: [],
    mcpServers: [],
    mcpCatalog: [],
    environments: [],
    // Mount is open: the catalogues start loading immediately, so the
    // initial status is 'loading' rather than set from inside the effect.
    status: {
      models: 'loading',
      toolScopes: 'loading',
      connections: 'loading',
      mcp: 'loading',
      environments: 'loading',
    },
    errors: {},
  });

  // ── Identity-mode create permissions ───────────────────────────────────────
  // Resolved from the session scope set the app already loads (the
  // PermissionsProvider caches the policy gateway's effective-scope
  // projection). Coverage uses the prefix-covers hierarchy, never string
  // equality: agents:types:write covers both mode-qualified children.
  const { scopes: sessionScopes, loaded: scopesLoaded } = usePermissions();
  const identityModeAccess = useMemo<IdentityModeAccess>(
    () => ({
      loaded: scopesLoaded,
      autonomous: !scopesLoaded || hasScopeCovering(sessionScopes, 'agents:types:autonomous:write'),
      delegated: !scopesLoaded || hasScopeCovering(sessionScopes, 'agents:types:delegated:write'),
    }),
    [scopesLoaded, sessionScopes],
  );

  // Derived, not stored: when the selected identity mode is one the caller
  // cannot create but the other mode is available, the selection resolves to
  // the available mode. This turns the static autonomous default into
  // "delegated by default" for delegated-only creators (and mirror-image),
  // including a resumed draft whose stored mode the caller cannot create.
  // When neither mode is available the selection stands: the identity step
  // disables both cards and the server refuses. The resolved fields feed the
  // UI, the draft saver, and submission alike.
  const resolvedFields = useMemo<BuilderFields>(() => {
    if (!identityModeAccess.loaded) return fields;
    if (
      fields.identityMode === 'autonomous' &&
      !identityModeAccess.autonomous &&
      identityModeAccess.delegated
    ) {
      return { ...fields, identityMode: 'delegated' };
    }
    if (
      fields.identityMode === 'delegated' &&
      !identityModeAccess.delegated &&
      identityModeAccess.autonomous
    ) {
      return { ...fields, identityMode: 'autonomous' };
    }
    return fields;
  }, [fields, identityModeAccess]);

  // Refs the debounced saver reads without re-arming on every keystroke.
  // Synced on commit — the debounce fires long after, so it always sees the
  // latest committed values.
  const fieldsRef = useRef(resolvedFields);
  const stepRef = useRef(step);
  const draftIdRef = useRef(draftId);
  useEffect(() => {
    fieldsRef.current = resolvedFields;
    stepRef.current = step;
    draftIdRef.current = draftId;
  });
  const dirtyRef = useRef(false);
  const savingRef = useRef(false);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const deadlineTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const loadedRef = useRef(false);

  // ── Catalogue load, once per mount ─────────────────────────────────────────
  useEffect(() => {
    if (loadedRef.current) return;
    loadedRef.current = true;
    void fetchModels().then(({ data, error }) =>
      setCatalogues((c) => ({
        ...c,
        models: data,
        status: { ...c.status, models: error ? 'error' : 'success' },
        errors: { ...c.errors, models: error ?? undefined },
      })),
    );
    void fetchToolScopes().then(({ data, error }) =>
      setCatalogues((c) => ({
        ...c,
        toolScopes: data,
        status: { ...c.status, toolScopes: error ? 'error' : 'success' },
        errors: { ...c.errors, toolScopes: error ?? undefined },
      })),
    );
    void fetchConnections().then(({ data, error }) =>
      setCatalogues((c) => ({
        ...c,
        connections: data,
        status: { ...c.status, connections: error ? 'error' : 'success' },
        errors: { ...c.errors, connections: error ?? undefined },
      })),
    );
    void fetchMcpCatalogue().then(({ data, error }) =>
      setCatalogues((c) => ({
        ...c,
        mcpServers: data.servers,
        mcpCatalog: data.catalog,
        status: { ...c.status, mcp: error ? 'error' : 'success' },
        errors: { ...c.errors, mcp: error ?? undefined },
      })),
    );
    void fetchEnvironments().then(({ data, error }) =>
      setCatalogues((c) => ({
        ...c,
        environments: data,
        status: { ...c.status, environments: error ? 'error' : 'success' },
        errors: { ...c.errors, environments: error ?? undefined },
      })),
    );
  }, []);

  // ── Draft resume ───────────────────────────────────────────────────────────
  // Guarded by draftId state: once the builder owns a draft (created or
  // resumed), a later prop change (URL adopting the id) must not refetch.
  useEffect(() => {
    if (!initialDraftId || draftId) return;
    let cancelled = false;
    void fetchDraft(initialDraftId).then(({ draft }) => {
      if (cancelled || !draft) return;
      const { fields: restored, step: restoredStep } = fromPayload(draft.payload as DraftPayload);
      setFields(restored);
      setStep(restoredStep);
      setMaxVisited(restoredStep);
      setDraftId(draft.id);
      setSaveStatus('saved');
    });
    return () => {
      cancelled = true;
    };
  }, [initialDraftId, draftId]);

  // ── Debounced draft persistence ────────────────────────────────────────────
  const flush = useCallback(async () => {
    if (savingRef.current) return;
    savingRef.current = true;
    if (deadlineTimer.current) {
      clearTimeout(deadlineTimer.current);
      deadlineTimer.current = null;
    }
    // Loop: changes that land while a write is in flight re-dirty the draft,
    // so keep writing until a pass completes with nothing new.
    while (dirtyRef.current) {
      dirtyRef.current = false;
      setSaveStatus('saving');
      const payload = toPayload(fieldsRef.current, stepRef.current);
      let result: { draft: AgentTypeDraft | null; error: string | null };
      if (draftIdRef.current) {
        result = await updateDraft(draftIdRef.current, payload);
      } else {
        result = await createDraft(payload);
        if (result.draft) {
          // The modal watches draftId and adopts it into the URL (?agent={id}).
          draftIdRef.current = result.draft.id;
          setDraftId(result.draft.id);
        }
      }
      if (result.error) {
        dirtyRef.current = true;
        savingRef.current = false;
        setSaveStatus('error');
        return;
      }
    }
    savingRef.current = false;
    setSaveStatus('saved');
  }, []);

  const scheduleSave = useCallback(() => {
    dirtyRef.current = true;
    setSaveStatus('unsaved');
    if (saveTimer.current) clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => void flush(), SAVE_DEBOUNCE_MS);
    if (!deadlineTimer.current) {
      deadlineTimer.current = setTimeout(() => {
        deadlineTimer.current = null;
        void flush();
      }, SAVE_DEADLINE_MS);
    }
  }, [flush]);

  useEffect(
    () => () => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
      if (deadlineTimer.current) clearTimeout(deadlineTimer.current);
    },
    [],
  );

  // A hard reload or tab close can land inside the debounce window and lose
  // the last change. pagehide fires in time for a keepalive request, which
  // outlives the page — but only for an existing draft (a create returns an id
  // nothing would receive).
  useEffect(() => {
    const flushOnHide = () => {
      if (!dirtyRef.current || !draftIdRef.current) return;
      void fetch(ApiRoute.agentTypes.draft(draftIdRef.current), {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ payload: toPayload(fieldsRef.current, stepRef.current) }),
        keepalive: true,
      }).catch(() => undefined);
    };
    window.addEventListener('pagehide', flushOnHide);
    return () => window.removeEventListener('pagehide', flushOnHide);
  }, []);

  // ── Field setters ──────────────────────────────────────────────────────────
  const setField = useCallback(
    <K extends keyof BuilderFields>(key: K, value: BuilderFields[K]) => {
      setFields((f) => ({ ...f, [key]: value }));
      scheduleSave();
    },
    [scheduleSave],
  );

  const setName = useCallback(
    (name: string) => {
      setFields((f) => ({ ...f, name, slug: f.slugEdited ? f.slug : deriveSlug(name) }));
      scheduleSave();
    },
    [scheduleSave],
  );

  const setSlug = useCallback(
    (slug: string) => {
      setFields((f) => ({ ...f, slug: deriveSlug(slug) || slug.toLowerCase(), slugEdited: true }));
      scheduleSave();
    },
    [scheduleSave],
  );

  // ── Navigation + validity ──────────────────────────────────────────────────
  const stepValid = useMemo(() => {
    if (step === 0) {
      return fields.name.trim().length > 1 && SLUG_RE.test(fields.slug);
    }
    return true;
  }, [step, fields.name, fields.slug]);

  const stepHint = useMemo((): string | undefined => {
    if (step === 0) {
      if (fields.name.trim().length <= 1) return 'Enter a display name of at least two characters.';
      if (!SLUG_RE.test(fields.slug))
        return 'Slug must be lowercase letters, digits, and hyphens only.';
    }
    if (step === 1) {
      if (!fields.modelId) return 'Choose a model before continuing.';
      if (!fields.systemPrompt.trim()) return 'Add system instructions before continuing.';
    }
    return undefined;
  }, [step, fields.name, fields.slug, fields.modelId, fields.systemPrompt]);

  const createBlockers = useMemo(() => {
    const blockers: string[] = [];
    if (!(fields.name.trim().length > 1)) blockers.push('Identity needs a display name.');
    if (!SLUG_RE.test(fields.slug)) blockers.push('Identity needs a valid slug.');
    if (!fields.modelId) blockers.push('Behaviour needs a model.');
    if (!fields.systemPrompt.trim()) blockers.push('Behaviour needs system instructions.');
    return blockers;
  }, [fields.name, fields.slug, fields.modelId, fields.systemPrompt]);

  const goTo = useCallback(
    (target: number) => {
      const clamped = Math.min(Math.max(target, 0), BUILDER_STEPS.length - 1);
      if (clamped > maxVisited + 1) return;
      setStep(clamped);
      setMaxVisited((m) => Math.max(m, clamped));
      scheduleSave();
    },
    [maxVisited, scheduleSave],
  );

  const next = useCallback(() => {
    if (!stepValid) return;
    goTo(stepRef.current + 1);
  }, [stepValid, goTo]);

  const back = useCallback(() => goTo(stepRef.current - 1), [goTo]);

  // ── Submit ─────────────────────────────────────────────────────────────────
  const submit = useCallback(async () => {
    if (submitting || createBlockers.length > 0) return;
    setSubmitting(true);
    setSubmitError(null);
    const payload = toPayload(fieldsRef.current, stepRef.current);
    delete payload._builder;
    const { agentType, error } = await createAgentType(payload as CreateAgentTypeRequest);
    if (error || !agentType) {
      setSubmitting(false);
      setSubmitError(error ?? 'The agent could not be created.');
      return;
    }
    if (draftIdRef.current) {
      // Best-effort: a lingering draft is reaped, and resuming it collides on
      // the now-taken slug with a clear error (ADR §4 Submission).
      void deleteDraft(draftIdRef.current);
    }
    setCreated(agentType);
    setSubmitting(false);
  }, [submitting, createBlockers]);

  const saveNow = useCallback(() => {
    if (saveTimer.current) clearTimeout(saveTimer.current);
    void flush();
  }, [flush]);

  return {
    step,
    maxVisited,
    fields: resolvedFields,
    catalogues,
    identityModeAccess,
    draftId,
    saveStatus,
    submitting,
    submitError,
    created,
    stepValid,
    stepHint,
    createBlockers,
    setField,
    setName,
    setSlug,
    goTo,
    next,
    back,
    saveNow,
    submit,
  };
}
