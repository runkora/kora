'use client';

/** @tier 3 — mcp-gateway-scoped. One server: tools-by-domain + Connect + Trust sidebar (spec-exact). */

import { useState } from 'react';
import type { McpCatalogTool } from '@orbit/shared/api';
import { Icon, SidebarLayout, Stack, Text, EmptyState } from '@orbit/hades';
import {
  MCP_CLIENTS,
  buildMcpConfig,
  isCommandClient,
  mcpServerUrl,
  type McpClient,
} from '@orbit/lib/mcp';
import type { DomainGroup, McpServer } from '../_types';
import { buildDomainGroups, relativeTime, serverRollupScope, statusColor } from '../_types';
import { ServerAvatar } from './server-avatar';
import { ConfigBlock } from './config-block';

// Hades caption/micro variants are leading-none; the spec composed these labels
// on the default 1.5 line box. `lineHeight` is kept inline where the line box
// height is load-bearing for the surrounding layout (stacked rows, chip rows).
// Font sizes 13/15/17/26px sit more than a sub-pixel from any Text/Heading
// variant, so those spec values are kept verbatim rather than snapped.

interface McpServerDetailPageProps {
  serverSlug: string;
  server: McpServer | null;
  tools: McpCatalogTool[];
  error: string | null;
}

function healthLabel(status: string): string {
  if (status === 'healthy') return 'Healthy';
  if (status === 'stale') return 'Stale';
  return 'Unknown';
}

export function McpServerDetailPage({
  serverSlug,
  server,
  tools,
  error,
}: McpServerDetailPageProps) {
  if (error || !server) {
    return (
      <EmptyState
        icon="server"
        title={error ? 'Could not load server' : 'Server not found'}
        description={
          error ??
          `No MCP server named "${serverSlug}" is visible for this tenant. It may not be registered, or you may lack access.`
        }
      />
    );
  }

  const groups = buildDomainGroups(tools, serverSlug);
  const accent = server.name === 'orbit';
  const hColor = statusColor(server.status);

  return (
    <Stack gap="relaxed">
      {/* Identity header */}
      <Stack direction="horizontal" align="start" gap="comfortable">
        <ServerAvatar name={server.name} accent={accent} size="lg" />
        <Stack grow gap="none" style={{ minWidth: 0 }}>
          <Stack direction="horizontal" align="center" style={{ gap: 10 }} wrap>
            <Text as="span" weight="heavy" style={{ fontSize: 26, letterSpacing: '-0.02em' }}>
              {server.display_name}
            </Text>
            <Text
              as="span"
              size="caption"
              weight="strong"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                height: 24,
                padding: '0 9px',
                borderRadius: 'var(--radius-full)',
                color: hColor,
                background: `color-mix(in oklch, ${hColor} 14%, transparent)`,
                border: `1px solid color-mix(in oklch, ${hColor} 34%, transparent)`,
              }}
            >
              <Text
                as="span"
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: 'var(--radius-full)',
                  background: hColor,
                  display: 'inline-block',
                  animation: 'mcp-pulse 2s ease-in-out infinite',
                }}
              />
              {healthLabel(server.status)}
            </Text>
          </Stack>
          <Stack direction="horizontal" align="center" style={{ gap: 10, marginTop: 6 }}>
            <Text as="span" mono weight="strong" color="muted" style={{ fontSize: 13 }}>
              {server.name}
            </Text>
            <Text as="span" style={{ color: 'var(--color-border)' }}>
              ·
            </Text>
            <Text as="span" color="subtle" style={{ fontSize: 13 }}>
              {server.global ? 'Global' : 'Tenant'} server
            </Text>
          </Stack>
          {server.description ? (
            <Text
              size="supporting"
              color="muted"
              style={{ margin: '10px 0 0', lineHeight: 1.55, maxWidth: 620 }}
            >
              {server.description}
            </Text>
          ) : null}
        </Stack>
      </Stack>

      {/* Two-column body */}
      <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
        {/* Main — tools */}
        <SidebarLayout.Main>
          <Stack gap="comfortable" style={{ minWidth: 0 }}>
            <Stack direction="horizontal" align="baseline" justify="between">
              <Text as="span" weight="strong" style={{ fontSize: 17 }}>
                Tools
              </Text>
              <Text as="span" size="caption" color="subtle">
                {tools.length} across {groups.length} domain{groups.length === 1 ? '' : 's'}
              </Text>
            </Stack>

            {/* Whole-server grant */}
            <Stack
              direction="horizontal"
              align="center"
              style={{
                gap: 12,
                padding: '13px 16px',
                borderRadius: 'var(--radius-lg)',
                background: 'var(--color-accent-subtle)',
                border: '1px solid var(--color-accent-border)',
              }}
            >
              <Icon name="shield-check" color="accent" size="sm" />
              <Stack grow gap="none" style={{ minWidth: 0 }}>
                <Text as="span" mono size="caption" weight="strong" style={{ lineHeight: 1.5 }}>
                  {serverRollupScope(server.name)}
                </Text>
                <Text
                  as="span"
                  size="caption"
                  color="muted"
                  style={{ marginTop: 2, lineHeight: 1.5 }}
                >
                  Whole-server grant: allows every tool below
                </Text>
              </Stack>
            </Stack>

            {groups.length === 0 ? (
              <Text as="span" color="muted" style={{ fontSize: 13 }}>
                No tools have been introspected for this server yet.
              </Text>
            ) : (
              groups.map((g) => <DomainCard key={g.domain} group={g} />)
            )}
          </Stack>
        </SidebarLayout.Main>

        {/* Sidebar — connect + trust */}
        <SidebarLayout.Sidebar>
          <Stack gap="comfortable" style={{ position: 'sticky', top: 0 }}>
            <ConnectCard slug={server.name} delegated={server.caller_identity === 'forward'} />
            <TrustCard server={server} />
          </Stack>
        </SidebarLayout.Sidebar>
      </SidebarLayout>
    </Stack>
  );
}

function DomainCard({ group }: { group: DomainGroup }) {
  return (
    <Stack
      style={{
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
        background: 'var(--color-surface)',
      }}
    >
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 10,
          padding: '12px 16px',
          borderBottom: '1px solid var(--color-border-subtle)',
          background: 'var(--color-surface-elevated)',
        }}
      >
        <Text as="span" size="supporting" weight="strong" style={{ textTransform: 'capitalize' }}>
          {group.domain}
        </Text>
        <Text
          as="span"
          mono
          size="micro"
          weight="emphasis"
          color="subtle"
          style={{
            lineHeight: 1.5,
            padding: '2px 7px',
            borderRadius: 'var(--radius-sm)',
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border-subtle)',
          }}
        >
          {group.rollupScope}
        </Text>
        <Text as="span" size="micro" color="subtle" style={{ marginLeft: 'auto' }}>
          {group.tools.length} tools
        </Text>
      </Stack>
      {group.tools.map((t, i) => (
        <Stack
          key={t.toolName}
          direction="horizontal"
          align="start"
          style={{
            gap: 14,
            padding: '12px 16px',
            borderTop: i === 0 ? 'none' : '1px solid var(--color-border-subtle)',
          }}
        >
          <Stack gap="none" shrink style={{ width: 180 }}>
            <Text as="span" mono size="caption" weight="strong" style={{ lineHeight: 1.5 }}>
              {t.toolName}
            </Text>
            <Text
              as="span"
              mono
              size="micro"
              weight="emphasis"
              color="subtle"
              style={{ lineHeight: 1.5, marginTop: 3, wordBreak: 'break-all' }}
            >
              {t.scope}
            </Text>
          </Stack>
          <Text as="span" color="muted" style={{ flex: 1, fontSize: 13, lineHeight: 1.5 }}>
            {t.description || '—'}
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

function ConnectCard({ slug, delegated }: { slug: string; delegated: boolean }) {
  const [client, setClient] = useState<McpClient>('desktop');
  const config = buildMcpConfig(client, [slug]);
  const clientDef = MCP_CLIENTS.find((c) => c.id === client) ?? MCP_CLIENTS[0];
  const authNote = delegated
    ? 'You authorise via OAuth on first connect. A browser window opens. Agents receive a just-in-time injected token; no secret is stored in this config.'
    : 'The gateway injects a scoped service credential at call time. No secret is stored in this config, and the upstream credential is never exposed to the client.';

  return (
    <Stack
      style={{
        border: '1px solid var(--color-accent-border)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
        background: 'var(--color-surface)',
      }}
    >
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 9,
          padding: '14px 16px',
          borderBottom: '1px solid var(--color-border-subtle)',
        }}
      >
        <Icon name="terminal" color="accent" size="sm" />
        <Text as="span" size="supporting" weight="strong">
          Connect
        </Text>
        <Text as="span" size="micro" color="subtle" style={{ marginLeft: 'auto' }}>
          MCP client
        </Text>
      </Stack>
      <Stack style={{ padding: '14px 16px', gap: 12 }}>
        {/* Client mini-tabs — pick-one, but the spec draws a flat 26px segment with an
            overlay fill on the active item; Card selected / RadioCard would draw an
            accent border (and a radio dot), so the state stays token-styled here. */}
        <Stack
          direction="horizontal"
          style={{
            gap: 3,
            padding: 3,
            borderRadius: 'var(--radius-md)',
            background: 'var(--color-background)',
            border: '1px solid var(--color-border-subtle)',
          }}
        >
          {MCP_CLIENTS.map((c) => {
            const active = c.id === client;
            return (
              <Text
                as="span"
                key={c.id}
                role="button"
                onClick={() => setClient(c.id)}
                size="micro"
                weight={active ? 'strong' : 'emphasis'}
                color={active ? 'foreground' : 'subtle'}
                style={{
                  flex: 1,
                  height: 26,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  // 6px nested radius: no token between --radius-sm (4px) and
                  // --radius-md (8px), so the spec value is kept.
                  borderRadius: 6,
                  cursor: 'pointer',
                  whiteSpace: 'nowrap',
                  background: active ? 'var(--color-surface-overlay)' : 'transparent',
                }}
              >
                {c.short}
              </Text>
            );
          })}
        </Stack>
        <Text
          as="span"
          mono
          size="micro"
          weight="emphasis"
          color="subtle"
          style={{ lineHeight: 1.5 }}
        >
          {clientDef.file}
        </Text>
        <ConfigBlock text={config} command={isCommandClient(client)} />
        <Stack
          direction="horizontal"
          style={{
            gap: 9,
            padding: '11px 12px',
            borderRadius: 'var(--radius-md)',
            background: 'var(--color-accent-subtle)',
            border: '1px solid var(--color-accent-border)',
          }}
        >
          <Icon name="info" color="accent" size="xs" />
          <Text as="span" size="micro" color="muted" style={{ lineHeight: 1.5 }}>
            {authNote}
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}

function TrustCard({ server }: { server: McpServer }) {
  const delegated = server.caller_identity === 'forward';
  const idColor = delegated ? 'var(--color-accent)' : 'var(--color-muted)';
  const meta: { k: string; v: string; color: string }[] = [
    { k: 'Transport', v: server.transport, color: 'var(--color-muted)' },
    { k: 'Endpoint', v: mcpServerUrl(server.name), color: 'var(--color-accent)' },
    {
      k: 'Scope',
      v: server.global ? 'Global · all tenants' : 'Tenant-scoped',
      color: 'var(--color-muted)',
    },
    { k: 'Last synced', v: relativeTime(server.last_synced), color: 'var(--color-muted)' },
  ];

  return (
    <Stack
      style={{
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
        background: 'var(--color-surface)',
      }}
    >
      <Text
        size="supporting"
        weight="strong"
        style={{
          padding: '13px 16px',
          borderBottom: '1px solid var(--color-border-subtle)',
        }}
      >
        Trust &amp; connection
      </Text>
      <Stack style={{ padding: '14px 16px', gap: 14 }}>
        <Stack direction="horizontal" style={{ gap: 11 }}>
          <Text
            as="span"
            style={{
              width: 32,
              height: 32,
              flexShrink: 0,
              borderRadius: 'var(--radius-md)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: idColor,
              background: delegated ? 'var(--color-accent-muted)' : 'var(--color-surface-overlay)',
              border: `1px solid ${delegated ? 'var(--color-accent-border)' : 'var(--color-border-subtle)'}`,
            }}
          >
            <Icon name={delegated ? 'user' : 'box'} size="sm" />
          </Text>
          <Stack grow gap="none" style={{ minWidth: 0 }}>
            <Text as="span" weight="strong" style={{ fontSize: 13, color: idColor }}>
              {delegated ? 'Delegated' : 'Service'} identity
            </Text>
            <Text as="span" size="caption" color="muted" style={{ lineHeight: 1.5, marginTop: 3 }}>
              {delegated
                ? 'Acts as you: it inherits your kora identity and is bound by exactly your grants. It can never exceed what you can do.'
                : 'Acts as itself: the gateway injects a dedicated service credential just-in-time. It never sees your identity or tokens.'}
            </Text>
          </Stack>
        </Stack>

        <Stack style={{ height: 1, background: 'var(--color-border-subtle)' }} />

        <Stack style={{ gap: 11 }}>
          {meta.map((m) => (
            <Stack
              key={m.k}
              direction="horizontal"
              align="center"
              justify="between"
              style={{ gap: 12 }}
            >
              <Text as="span" size="caption" color="subtle" style={{ lineHeight: 1.5 }}>
                {m.k}
              </Text>
              <Text
                as="span"
                mono
                size="caption"
                weight="emphasis"
                align="end"
                style={{ lineHeight: 1.5, color: m.color, wordBreak: 'break-all' }}
              >
                {m.v}
              </Text>
            </Stack>
          ))}
        </Stack>

        <Stack
          direction="horizontal"
          align="center"
          style={{
            gap: 7,
            padding: '9px 10px',
            borderRadius: 'var(--radius-md)',
            background: 'var(--color-surface-overlay)',
            border: '1px dashed var(--color-border-subtle)',
          }}
        >
          <Icon name="info" color="subtle" size="2xs" />
          <Text as="span" size="micro" color="subtle" style={{ lineHeight: 1.5 }}>
            Reachability &amp; latency checks: coming soon
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}
