'use client';

/** @tier 3 — agents/[slug]-scoped. Connectivity tab. Two-column layout: the
 * three editable layers (egress posture, connectors, environment) on the left as
 * compact cards with their own save/dirty/discard state; the resolved
 * "Effective access" panel on the right, sticky and live — it re-resolves the
 * draft as the user edits, before any save. */

import { useMemo, useState } from 'react';
import Link from 'next/link';
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Form,
  Icon,
  Stack,
  Text,
} from '@orbit/terra';
import type { Environment } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentConnectivity } from '../_hooks/use-agent-connectivity';
import { hostsToText, textToHosts } from '../../_components/connectivity-fields';
import { ConnectivityEgressField } from './connectivity-egress-field';
import { ConnectorsFields } from '../../_components/connectors-fields';
import { McpToolsFields } from '../../_components/mcp-tools-fields';
import { EnvironmentField } from '../../_components/environment-field';
import { EnvironmentSummary } from './environment-summary';
import { EffectiveAccessPanel } from './effective-access-panel';

// ── Compact layer card ──────────────────────────────────────────────────────────
// A tightened card for an editable layer: small icon + title + one muted
// description line, a compact header, and compact content padding so the left
// column reads as a dense, cohesive stack rather than three tall cards.

function LayerCard({
  icon,
  title,
  description,
  action,
  children,
}: {
  icon: 'network' | 'key' | 'layers' | 'server';
  title: string;
  description: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader
        compact
        leadingIcon={icon}
        title={title}
        description={description}
        action={action}
      />
      <CardContent compact>{children}</CardContent>
    </Card>
  );
}

export function AgentConnectivityTab() {
  const {
    agent,
    connectivityMode,
    setConnectivityMode,
    connectivityHosts,
    setConnectivityHosts,
    connectors,
    setConnectors,
    availableConnections,
    environmentId,
    setEnvironmentId,
    mcpTools,
    setMcpTools,
    mcpServers,
    mcpCatalog,
    effectiveAccess,
    accessLoading,
    accessError,
    isDirty,
    saving,
    saveError,
    saveSuccess,
    setSaveSuccess,
    handleSave,
    handleDiscard,
  } = useAgentConnectivity();

  // The tenant's environments, surfaced by EnvironmentField once it loads them.
  // Reused here to render a summary of the selected environment's contents
  // without issuing a second request. The field still owns the fetch and the
  // `value`/`onChange(id)` contract; this is purely presentational enrichment.
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const selectedEnvironment = useMemo(
    () => (environmentId ? (environments.find((env) => env.id === environmentId) ?? null) : null),
    [environments, environmentId],
  );

  if (!agent) return null;

  return (
    <Stack paddingBlock="comfortable">
      {/* No align="start": on mobile the stack is vertical, where flex-start
          sizes children to their content and overflows. Stretch keeps the
          columns full-width. */}
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="comfortable">
        {/* ── LEFT: editable layers (~60%) ──────────────────────────────────── */}
        <Stack grow gap="comfortable" minWidth="none">
          <Form
            id="connectivity-form"
            onSubmit={(e) => void handleSave(e)}
            noValidate
            gap="comfortable"
          >
            <LayerCard
              icon="network"
              title="Egress"
              description="Where this agent's sandboxes may reach on the internet."
            >
              <ConnectivityEgressField
                mode={connectivityMode}
                hosts={textToHosts(connectivityHosts)}
                onModeChange={(m) => {
                  setConnectivityMode(m);
                  setSaveSuccess(false);
                }}
                onHostsChange={(h) => {
                  setConnectivityHosts(hostsToText(h));
                  setSaveSuccess(false);
                }}
              />
            </LayerCard>

            <LayerCard
              icon="key"
              title="Connectors"
              description="Registered connections the gateway injects a scoped, short-lived credential for — the agent never holds the secret."
            >
              <ConnectorsFields
                available={availableConnections}
                value={connectors}
                onChange={(c) => {
                  setConnectors(c);
                  setSaveSuccess(false);
                }}
              />
            </LayerCard>

            <LayerCard
              icon="server"
              title="MCP tools"
              description="The exact tools this agent may call through the governed MCP gateway. Everything else is filtered from tools/list and rejected at call time."
            >
              <McpToolsFields
                servers={mcpServers}
                catalog={mcpCatalog}
                value={mcpTools}
                onChange={(t) => {
                  setMcpTools(t);
                  setSaveSuccess(false);
                }}
              />
            </LayerCard>

            <LayerCard
              icon="layers"
              title="Environment"
              description="A reusable bundle of egress hosts + connectors, folded into this agent's access."
              action={
                <Button variant="ghost" size="sm" asChild>
                  <Link href={APP_ROUTES.environments}>
                    Manage environments
                    <Icon name="arrow-right" size="xs" />
                  </Link>
                </Button>
              }
            >
              <Stack
                direction={{ base: 'vertical', sm: 'horizontal' }}
                gap="comfortable"
                align="start"
              >
                <Stack maxWidth="form-field" minWidth="none" shrink>
                  <EnvironmentField
                    compact
                    value={environmentId}
                    onChange={(id) => {
                      setEnvironmentId(id);
                      setSaveSuccess(false);
                    }}
                    onEnvironmentsLoaded={setEnvironments}
                  />
                </Stack>
                <Stack grow minWidth="none">
                  {selectedEnvironment ? (
                    <EnvironmentSummary environment={selectedEnvironment} />
                  ) : (
                    <Text size="supporting" color="muted">
                      No environment — this agent uses only its own egress and connectors.
                    </Text>
                  )}
                </Stack>
              </Stack>
            </LayerCard>

            {saveError && <Alert variant="error">{saveError}</Alert>}
          </Form>
        </Stack>

        {/* ── RIGHT: resolved view (~40%, sticky) ───────────────────────────── */}
        <Stack
          shrink
          minWidth={{ base: 'none', lg: 'card' }}
          maxWidth={{ base: 'full', lg: 'card' }}
          style={{ position: 'sticky', top: '16px', alignSelf: 'flex-start' }}
        >
          <EffectiveAccessPanel
            access={effectiveAccess}
            loading={accessLoading}
            error={accessError}
          />
        </Stack>
      </Stack>

      {/* ── Floating save bar ──────────────────────────────────────────────── */}
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
            border: `1px solid ${saveSuccess ? 'oklch(50% 0.14 145 / 0.6)' : 'var(--color-border)'}`,
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
                  form="connectivity-form"
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
    </Stack>
  );
}
