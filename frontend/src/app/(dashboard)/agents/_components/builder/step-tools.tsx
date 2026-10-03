'use client';

/** @tier 3 — agents-scoped. Builder step 3: MCP tool tree, connections, egress, environments. */

import { useEffect, useMemo, useState } from 'react';
import { Alert, SectionHeading, Separator, Stack, Text } from '@orbit/terra';
import type { EffectiveAccess } from '@orbit/shared/api';
import { previewEffectiveAccess } from '../../[slug]/_services/agent-detail-api';
import { ConnectionCards, EffectiveAccessPanel } from '@orbit/components';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { EgressModeCards } from '../motif/egress-mode-cards';
import { EnvironmentsCard } from '../motif/environments-card';
import { countGrantedServers, McpToolTree } from '../motif/mcp-tool-tree';

/** Section heading shared by every block on this step. */
function SectionLabel({ title, desc }: { title: string; desc: string }) {
  return (
    <Stack gap="tight">
      <Text as="span" size="caption" weight="heavy" color="muted">
        {title}
      </Text>
      <Text size="micro" color="subtle">
        {desc}
      </Text>
    </Stack>
  );
}

export function StepTools() {
  const { fields, setField, catalogues } = useAgentBuilderContext();

  const grantedServers = useMemo(
    () => countGrantedServers(catalogues.mcpServers, catalogues.mcpCatalog, fields.mcpTools),
    [catalogues.mcpServers, catalogues.mcpCatalog, fields.mcpTools],
  );

  // ── Effective access preview ─────────────────────────────────────────────
  // Resolved live as the user edits, so the scopes being assigned are visible
  // on the page that assigns them (same panel the connectivity tab uses).
  const [access, setAccess] = useState<EffectiveAccess | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
      void previewEffectiveAccess({
        connectivity: {
          mode: fields.connectivityMode,
          hosts: fields.connectivityHosts
            .split('\n')
            .map((h) => h.trim())
            .filter(Boolean),
        },
        connectors: fields.connectors,
        environments: fields.environments,
      }).then((result) => {
        setAccess(result.access);
        setAccessError(result.error);
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [fields.connectivityMode, fields.connectivityHosts, fields.connectors, fields.environments]);

  return (
    <Stack gap="relaxed">
      <SectionHeading
        icon="wrench"
        title="Tools & access"
        description="Grant access to MCP servers. Every tool call is checked against these scopes before it runs."
      />

      <Text as="span" size="supporting" color="muted">
        <Text as="span" size="supporting" weight="heavy" color="accent">
          {grantedServers}
        </Text>{' '}
        servers ·{' '}
        <Text as="span" size="supporting" weight="heavy">
          {fields.mcpTools.length}
        </Text>{' '}
        tools granted
      </Text>

      {catalogues.status.mcp === 'error' && (
        <Alert variant="error">MCP servers failed to load: {catalogues.errors.mcp}</Alert>
      )}

      {/* ── Server tree ────────────────────────────────────────────────────── */}
      <McpToolTree
        servers={catalogues.mcpServers}
        catalog={catalogues.mcpCatalog}
        granted={fields.mcpTools}
        onChange={(scopes) => setField('mcpTools', scopes)}
      />

      <Separator />

      {/* ── Connections ────────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel
          title="Connections"
          desc="Credentialed connections the agent may call. Credentials are injected just-in-time. The agent never sees them."
        />
        {catalogues.status.connections === 'error' && (
          <Alert variant="error">Connections failed to load: {catalogues.errors.connections}</Alert>
        )}
        {/* The empty text belongs to ConnectionCards; while the catalogue is
            still loading (or failed) nothing renders, exactly as before. */}
        {(catalogues.connections.length > 0 || catalogues.status.connections === 'success') && (
          <ConnectionCards
            available={catalogues.connections}
            value={fields.connectors}
            onChange={(connectors) => setField('connectors', connectors)}
          />
        )}
      </Stack>

      <Separator />

      {/* ── Network egress ─────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel title="Network egress" desc="The hosts the agent may reach directly." />
        <EgressModeCards
          mode={fields.connectivityMode}
          hosts={fields.connectivityHosts}
          onModeChange={(m) => setField('connectivityMode', m)}
          onHostsChange={(h) => setField('connectivityHosts', h)}
        />
      </Stack>

      <Separator />

      {/* ── Environments ───────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel
          title="Environments"
          desc="The sandboxes this agent can run in. Their egress hosts and connections join the agent's effective access."
        />
        {/* Until the catalogue loads, every attached environment would look deleted. */}
        {catalogues.status.environments !== 'loading' && (
          <EnvironmentsCard
            environments={catalogues.environments}
            value={fields.environments}
            onChange={(next) => setField('environments', next)}
            error={
              catalogues.status.environments === 'error' ? catalogues.errors.environments : null
            }
          />
        )}
      </Stack>

      <EffectiveAccessPanel access={access} loading={!access && !accessError} error={accessError} />
    </Stack>
  );
}
