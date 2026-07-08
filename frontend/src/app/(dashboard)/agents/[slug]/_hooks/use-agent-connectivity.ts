'use client';

/** @tier 3 — agents/[slug]-scoped. Business logic for the agent Connectivity
 * tab: the three connectivity layers (egress posture, connectors, environment)
 * plus the resolved effective-access view. Owns its own dirty/save/discard
 * state, independent of the settings form. Mirrors the connectivity slice of
 * use-agent-settings.ts. */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  Connectivity,
  ConnectivityMode,
  Connector,
  ConnectionCatalogEntry,
  EffectiveAccess,
  EffectiveAccessPreviewRequest,
  McpCatalogTool,
  McpServer,
} from '@orbit/shared/api';
import { previewEffectiveAccess, updateAgentType } from '../_services/agent-detail-api';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { hostsToText, textToHosts } from '../../_components/connectivity-fields';
import { normalizeConnectors } from './use-agent-settings';
import type { AgentTypeRow } from '../../_types';

// Debounce window for the live effective-access preview as the user edits.
const PREVIEW_DEBOUNCE_MS = 350;

export function useAgentConnectivity() {
  const { agent } = useAgentDetailContext();

  // ── Form state ──────────────────────────────────────────────────────────────
  const [connectivityMode, setConnectivityMode] = useState<ConnectivityMode>(
    agent?.connectivity.mode ?? 'none',
  );
  const [connectivityHosts, setConnectivityHosts] = useState(
    hostsToText(agent?.connectivity.hosts),
  );
  const [connectors, setConnectors] = useState<Connector[]>(agent?.connectors ?? []);
  const [availableConnections, setAvailableConnections] = useState<ConnectionCatalogEntry[]>([]);
  const [environmentId, setEnvironmentId] = useState(agent?.environmentId ?? '');
  const [mcpTools, setMcpTools] = useState<string[]>(agent?.mcpTools ?? []);
  const [mcpServers, setMcpServers] = useState<McpServer[]>([]);
  const [mcpCatalog, setMcpCatalog] = useState<McpCatalogTool[]>([]);

  // Tracks the last-saved values so isDirty resets immediately after a save.
  const [savedBaseline, setSavedBaseline] = useState({
    connectivityMode: agent?.connectivity.mode ?? ('none' as ConnectivityMode),
    connectivityHosts: hostsToText(agent?.connectivity.hosts),
    connectors: agent?.connectors ?? ([] as Connector[]),
    environmentId: agent?.environmentId ?? '',
    mcpTools: agent?.mcpTools ?? ([] as string[]),
  });

  // ── Connector catalog data ────────────────────────────────────────────────────
  useEffect(() => {
    fetch(ApiRoute.agentTypes.connectors(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: ConnectionCatalogEntry[] }) => {
        setAvailableConnections(body.data ?? []);
      })
      .catch(() => undefined);
  }, []);

  // ── Governed MCP tool catalog ─────────────────────────────────────────────────
  useEffect(() => {
    fetch(ApiRoute.agentTypes.mcpTools(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { servers?: McpServer[]; catalog?: McpCatalogTool[] }) => {
        setMcpServers(body.servers ?? []);
        setMcpCatalog(body.catalog ?? []);
      })
      .catch(() => undefined);
  }, []);

  // ── Effective access (live, draft-driven preview) ─────────────────────────────
  const agentId = agent?.id ?? '';
  const [effectiveAccess, setEffectiveAccess] = useState<EffectiveAccess | null>(null);
  const [accessLoading, setAccessLoading] = useState(true);
  const [accessError, setAccessError] = useState<string | null>(null);

  // The current draft, shaped for the preview endpoint. Recomputed on every edit
  // so the resolved panel can reflect unsaved changes before a save lands.
  const draftConnectivity: Connectivity = useMemo(
    () =>
      connectivityMode === 'whitelist'
        ? { mode: 'whitelist', hosts: textToHosts(connectivityHosts) }
        : { mode: connectivityMode },
    [connectivityMode, connectivityHosts],
  );

  // Debounced live preview. Each draft change schedules a POST of the current
  // draft after PREVIEW_DEBOUNCE_MS; an `ignore` guard drops stale responses so a
  // slow request can never clobber a newer one. `draftConnectivity` is memoised,
  // so the effect re-runs only when the resolved inputs actually change.
  useEffect(() => {
    if (!agentId) return;
    let ignore = false;
    const body: EffectiveAccessPreviewRequest = {
      connectivity: draftConnectivity,
      connectors,
      // Empty string ("") means "no environment"; send it as such so the
      // preview matches save semantics.
      environment_id: environmentId,
    };
    const timer = setTimeout(() => {
      previewEffectiveAccess(body).then(({ access, error }) => {
        if (ignore) return;
        setEffectiveAccess(access);
        setAccessError(error);
        setAccessLoading(false);
      });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      ignore = true;
      clearTimeout(timer);
    };
  }, [agentId, draftConnectivity, connectors, environmentId]);

  // ── Derived ─────────────────────────────────────────────────────────────────
  const isDirty =
    connectivityMode !== savedBaseline.connectivityMode ||
    connectivityHosts !== savedBaseline.connectivityHosts ||
    environmentId !== savedBaseline.environmentId ||
    JSON.stringify(normalizeConnectors(connectors)) !==
      JSON.stringify(normalizeConnectors(savedBaseline.connectors)) ||
    JSON.stringify([...mcpTools].sort()) !== JSON.stringify([...savedBaseline.mcpTools].sort());

  // ── Save / status ───────────────────────────────────────────────────────────
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const saveSuccessTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleSave = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!agent) return;
      setSaving(true);
      setSaveError(null);
      setSaveSuccess(false);
      const result = await updateAgentType(agent.id, {
        connectivity:
          connectivityMode === 'whitelist'
            ? { mode: 'whitelist', hosts: textToHosts(connectivityHosts) }
            : { mode: connectivityMode },
        connectors,
        // Replace the governed MCP tool grants; the backend composes them (and
        // derives the coarse mcp:invoke gate) into the effective grant.
        mcp_tools: mcpTools,
        // Only send environment_id when it changed: a non-empty id attaches,
        // an empty string ("") detaches, and omitting it leaves it unchanged.
        ...(environmentId !== savedBaseline.environmentId ? { environment_id: environmentId } : {}),
      });
      setSaving(false);
      if (result.error) {
        setSaveError(result.error);
        return;
      }
      // Update the baseline so isDirty immediately resets to false.
      setSavedBaseline({
        connectivityMode,
        connectivityHosts,
        connectors,
        environmentId,
        mcpTools,
      });
      setSaveSuccess(true);
      if (saveSuccessTimerRef.current) clearTimeout(saveSuccessTimerRef.current);
      saveSuccessTimerRef.current = setTimeout(() => setSaveSuccess(false), 2000);
      // The resolved panel already reflects the saved state — the live preview
      // tracks the same draft we just persisted, so no refetch is needed.
    },
    [
      agent,
      connectivityMode,
      connectivityHosts,
      connectors,
      mcpTools,
      environmentId,
      savedBaseline,
    ],
  );

  const handleDiscard = useCallback(() => {
    if (!agent) return;
    setConnectivityMode(savedBaseline.connectivityMode);
    setConnectivityHosts(savedBaseline.connectivityHosts);
    setConnectors(savedBaseline.connectors);
    setEnvironmentId(savedBaseline.environmentId);
    setMcpTools(savedBaseline.mcpTools);
    setSaveError(null);
  }, [agent, savedBaseline]);

  return {
    agent: agent as AgentTypeRow | null,
    // form fields
    connectivityMode,
    setConnectivityMode,
    connectivityHosts,
    setConnectivityHosts,
    connectors,
    setConnectors,
    availableConnections,
    environmentId,
    setEnvironmentId,
    // governed MCP tools
    mcpTools,
    setMcpTools,
    mcpServers,
    mcpCatalog,
    // effective access
    effectiveAccess,
    accessLoading,
    accessError,
    // derived
    isDirty,
    // save status
    saving,
    saveError,
    saveSuccess,
    setSaveSuccess,
    // handlers
    handleSave,
    handleDiscard,
  };
}
