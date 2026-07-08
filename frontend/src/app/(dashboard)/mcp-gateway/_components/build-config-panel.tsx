/** @tier 3 — mcp-gateway-scoped. Multi-server "Build config" side panel (spec-exact). */
'use client';

import { useState } from 'react';
import { Button, Icon, SidePanel, SidePanelAccessibleTitle, Stack, Text } from '@orbit/terra';
import { MCP_CLIENTS, buildMcpConfig, isCommandClient, type McpClient } from '@orbit/lib/mcp';
import type { McpServer } from '../_types';
import { serverRollupScope } from '../_types';
import { ServerAvatar } from './server-avatar';
import { ConfigBlock } from './config-block';

interface BuildConfigPanelProps {
  servers: McpServer[];
  selected: string[];
  onClose: () => void;
}

function SecLabel({ children }: { children: string }) {
  return (
    <Text
      as="span"
      style={{
        display: 'block',
        fontSize: 10,
        fontWeight: 600,
        letterSpacing: '0.13em',
        color: 'var(--color-subtle)',
      }}
    >
      {children}
    </Text>
  );
}

export function BuildConfigPanel({ servers, selected, onClose }: BuildConfigPanelProps) {
  const [client, setClient] = useState<McpClient>('desktop');

  const chosen = servers.filter((s) => selected.includes(s.name));
  const config = buildMcpConfig(client, selected);
  const clientDef = MCP_CLIENTS.find((c) => c.id === client) ?? MCP_CLIENTS[0];

  return (
    <SidePanel open width={472} onClose={onClose}>
      <SidePanelAccessibleTitle>Build config</SidePanelAccessibleTitle>

      {/* Header — 56px */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          height: 56,
          flexShrink: 0,
          gap: 10,
          padding: '0 18px',
          borderBottom: '1px solid var(--color-border-subtle)',
        }}
      >
        <Icon name="terminal" color="accent" size="sm" />
        <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
          <Text as="span" style={{ fontSize: 14, fontWeight: 600 }}>
            Build config
          </Text>
          <Text as="span" style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
            {chosen.length} server{chosen.length === 1 ? '' : 's'} · one gateway endpoint
          </Text>
        </Stack>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close panel">
          <Icon name="close" size="sm" />
        </Button>
      </Stack>

      {/* Body */}
      <Stack style={{ flex: 1, overflowY: 'auto', padding: 18, gap: 16 }}>
        {/* CLIENT */}
        <Stack style={{ gap: 8 }}>
          <SecLabel>CLIENT</SecLabel>
          <Stack style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
            {MCP_CLIENTS.map((c) => {
              const active = c.id === client;
              return (
                <Button
                  key={c.id}
                  variant="ghost"
                  onClick={() => setClient(c.id)}
                  style={{
                    height: 34,
                    width: '100%',
                    gap: 7,
                    justifyContent: 'center',
                    borderRadius: 'var(--radius-md)',
                    fontSize: 12.5,
                    fontWeight: active ? 600 : 500,
                    border: `1px solid ${active ? 'var(--color-border)' : 'transparent'}`,
                    background: active ? 'var(--color-surface-overlay)' : 'transparent',
                    color: active ? 'var(--color-foreground)' : 'var(--color-subtle)',
                  }}
                >
                  <Icon name={c.icon} size="xs" />
                  {c.label}
                </Button>
              );
            })}
          </Stack>
        </Stack>

        {/* CONFIG */}
        <Stack style={{ gap: 7 }}>
          <Stack direction="horizontal" align="center" justify="between">
            <SecLabel>CONFIG</SecLabel>
            <Text
              as="span"
              mono
              style={{ fontSize: 11, fontWeight: 500, color: 'var(--color-subtle)' }}
            >
              {clientDef.file}
            </Text>
          </Stack>
          <ConfigBlock text={config} command={isCommandClient(client)} />
        </Stack>

        {/* Authentication */}
        <Stack
          direction="horizontal"
          style={{
            gap: 10,
            padding: '12px 13px',
            borderRadius: 'var(--radius-md)',
            background: 'var(--color-accent-subtle)',
            border: '1px solid var(--color-accent-border)',
          }}
        >
          <Icon name="shield" color="accent" size="sm" />
          <Stack style={{ gap: 2 }}>
            <Text as="span" style={{ fontSize: 12.5, fontWeight: 600 }}>
              Authentication
            </Text>
            <Text as="span" style={{ fontSize: 12, lineHeight: 1.5, color: 'var(--color-muted)' }}>
              Humans authorise via OAuth on first connect — a browser window opens. Agents receive a
              just-in-time injected token; no secret is stored in this file.
            </Text>
          </Stack>
        </Stack>

        {/* GRANTS REQUIRED */}
        <Stack style={{ gap: 8 }}>
          <SecLabel>GRANTS REQUIRED</SecLabel>
          <Stack style={{ gap: 7 }}>
            {chosen.map((s) => (
              <Stack
                key={s.name}
                direction="horizontal"
                align="center"
                style={{
                  gap: 9,
                  padding: '8px 10px',
                  borderRadius: 'var(--radius-md)',
                  background: 'var(--color-background)',
                  border: '1px solid var(--color-border-subtle)',
                }}
              >
                <ServerAvatar name={s.name} accent={s.name === 'orbit'} size="xs" />
                <Text
                  as="span"
                  mono
                  style={{ flex: 1, fontSize: 12, fontWeight: 500, color: 'var(--color-muted)' }}
                >
                  {serverRollupScope(s.name)}
                </Text>
                <Text as="span" style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
                  {s.caller_identity === 'forward' ? 'Delegated' : 'Service'}
                </Text>
              </Stack>
            ))}
          </Stack>
          <Text
            as="span"
            style={{
              display: 'block',
              margin: '2px 2px 0',
              fontSize: 11.5,
              lineHeight: 1.5,
              color: 'var(--color-subtle)',
            }}
          >
            These grants are requested when you connect. Anything outside them is filtered from{' '}
            <Text as="span" mono style={{ fontSize: 11.5 }}>
              tools/list
            </Text>{' '}
            and rejected at call time.
          </Text>
        </Stack>
      </Stack>

      {/* Footer */}
      <Stack
        style={{
          flexShrink: 0,
          padding: '14px 18px',
          borderTop: '1px solid var(--color-border-subtle)',
        }}
      >
        <Button
          onClick={() => void navigator.clipboard.writeText(config)}
          style={{
            width: '100%',
            height: 40,
            borderRadius: 'var(--radius-md)',
            fontSize: 13.5,
            fontWeight: 600,
          }}
        >
          <Icon name="copy" size="sm" />
          Copy config
        </Button>
      </Stack>
    </SidePanel>
  );
}
