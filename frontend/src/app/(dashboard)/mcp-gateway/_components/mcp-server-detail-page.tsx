'use client';

/** @tier 3 — mcp-gateway-scoped. One server: tools-by-domain + Connect + Trust sidebar (spec-exact). */

import { useState } from 'react';
import type { McpCatalogTool } from '@orbit/shared/api';
import { Icon, Stack, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
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
    <Stack style={{ gap: 24 }}>
      {/* Identity header */}
      <Stack direction="horizontal" align="start" style={{ gap: 16 }}>
        <ServerAvatar name={server.name} accent={accent} size="lg" />
        <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
          <Stack direction="horizontal" align="center" style={{ gap: 10 }} wrap>
            <Text as="span" style={{ fontSize: 26, fontWeight: 700, letterSpacing: '-0.02em' }}>
              {server.display_name}
            </Text>
            <Text
              as="span"
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                height: 24,
                padding: '0 9px',
                borderRadius: 'var(--radius-full)',
                fontSize: 12,
                fontWeight: 600,
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
                  borderRadius: 9999,
                  background: hColor,
                  display: 'inline-block',
                  animation: 'mcp-pulse 2s ease-in-out infinite',
                }}
              />
              {healthLabel(server.status)}
            </Text>
          </Stack>
          <Stack direction="horizontal" align="center" style={{ gap: 10, marginTop: 6 }}>
            <Text
              as="span"
              mono
              style={{ fontSize: 13, fontWeight: 600, color: 'var(--color-muted)' }}
            >
              {server.name}
            </Text>
            <Text as="span" style={{ color: 'var(--color-border)' }}>
              ·
            </Text>
            <Text as="span" style={{ fontSize: 13, color: 'var(--color-subtle)' }}>
              {server.global ? 'Global' : 'Tenant'} server
            </Text>
          </Stack>
          {server.description ? (
            <Text
              as="span"
              style={{
                display: 'block',
                margin: '10px 0 0',
                fontSize: 14,
                lineHeight: 1.55,
                color: 'var(--color-muted)',
                maxWidth: 620,
              }}
            >
              {server.description}
            </Text>
          ) : null}
        </Stack>
      </Stack>

      {/* Two-column body */}
      <Stack
        style={{ display: 'grid', gridTemplateColumns: '1fr 372px', gap: 24, alignItems: 'start' }}
      >
        {/* Main — tools */}
        <Stack style={{ gap: 16, minWidth: 0 }}>
          <Stack direction="horizontal" align="baseline" justify="between">
            <Text as="span" style={{ fontSize: 17, fontWeight: 600 }}>
              Tools
            </Text>
            <Text as="span" style={{ fontSize: 12, color: 'var(--color-subtle)' }}>
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
            <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
              <Text as="span" mono style={{ fontSize: 12, fontWeight: 600 }}>
                {serverRollupScope(server.name)}
              </Text>
              <Text as="span" style={{ fontSize: 12, color: 'var(--color-muted)', marginTop: 2 }}>
                Whole-server grant — allows every tool below
              </Text>
            </Stack>
          </Stack>

          {groups.length === 0 ? (
            <Text as="span" style={{ fontSize: 13, color: 'var(--color-muted)' }}>
              No tools have been introspected for this server yet.
            </Text>
          ) : (
            groups.map((g) => <DomainCard key={g.domain} group={g} />)
          )}
        </Stack>

        {/* Sidebar — connect + trust */}
        <Stack style={{ gap: 16, position: 'sticky', top: 0 }}>
          <ConnectCard slug={server.name} delegated={server.caller_identity === 'forward'} />
          <TrustCard server={server} />
        </Stack>
      </Stack>
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
        <Text as="span" style={{ fontSize: 14, fontWeight: 600, textTransform: 'capitalize' }}>
          {group.domain}
        </Text>
        <Text
          as="span"
          mono
          style={{
            fontSize: 11,
            fontWeight: 500,
            color: 'var(--color-subtle)',
            padding: '2px 7px',
            borderRadius: 'var(--radius-sm)',
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border-subtle)',
          }}
        >
          {group.rollupScope}
        </Text>
        <Text as="span" style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--color-subtle)' }}>
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
          <Stack style={{ width: 180, flexShrink: 0, gap: 0 }}>
            <Text as="span" mono style={{ fontSize: 12.5, fontWeight: 600 }}>
              {t.toolName}
            </Text>
            <Text
              as="span"
              mono
              style={{
                fontSize: 11,
                fontWeight: 500,
                color: 'var(--color-subtle)',
                marginTop: 3,
                wordBreak: 'break-all',
              }}
            >
              {t.scope}
            </Text>
          </Stack>
          <Text
            as="span"
            style={{ flex: 1, fontSize: 13, lineHeight: 1.5, color: 'var(--color-muted)' }}
          >
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
    ? 'You authorise via OAuth on first connect — a browser window opens. Agents receive a just-in-time injected token; no secret is stored in this config.'
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
        <Text as="span" style={{ fontSize: 14, fontWeight: 600 }}>
          Connect
        </Text>
        <Text as="span" style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--color-subtle)' }}>
          MCP client
        </Text>
      </Stack>
      <Stack style={{ padding: '14px 16px', gap: 12 }}>
        {/* Client mini-tabs */}
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
                style={{
                  flex: 1,
                  height: 26,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRadius: 6,
                  cursor: 'pointer',
                  fontSize: 11,
                  fontWeight: active ? 600 : 500,
                  whiteSpace: 'nowrap',
                  background: active ? 'var(--color-surface-overlay)' : 'transparent',
                  color: active ? 'var(--color-foreground)' : 'var(--color-subtle)',
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
          style={{ fontSize: 11, fontWeight: 500, color: 'var(--color-subtle)' }}
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
          <Text as="span" style={{ fontSize: 11.5, lineHeight: 1.5, color: 'var(--color-muted)' }}>
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
        as="span"
        style={{
          display: 'block',
          padding: '13px 16px',
          borderBottom: '1px solid var(--color-border-subtle)',
          fontSize: 14,
          fontWeight: 600,
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
          <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
            <Text as="span" style={{ fontSize: 13, fontWeight: 600, color: idColor }}>
              {delegated ? 'Delegated' : 'Service'} identity
            </Text>
            <Text
              as="span"
              style={{ fontSize: 12, lineHeight: 1.5, color: 'var(--color-muted)', marginTop: 3 }}
            >
              {delegated
                ? 'Acts as you — it inherits your Orbit identity and is bound by exactly your grants. It can never exceed what you can do.'
                : 'Acts as itself — the gateway injects a dedicated service credential just-in-time. It never sees your identity or tokens.'}
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
              <Text as="span" style={{ fontSize: 12, color: 'var(--color-subtle)' }}>
                {m.k}
              </Text>
              <Text
                as="span"
                mono
                style={{
                  fontSize: 12,
                  fontWeight: 500,
                  color: m.color,
                  textAlign: 'right',
                  wordBreak: 'break-all',
                }}
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
          <Text as="span" style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
            Reachability &amp; latency checks — coming soon
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}
