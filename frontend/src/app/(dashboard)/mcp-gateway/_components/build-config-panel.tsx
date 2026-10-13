/** @tier 3 — mcp-gateway-scoped. Multi-server "Build config" side panel (spec-exact). */
'use client';

import { useState } from 'react';
import {
  CopyButton,
  Button,
  Eyebrow,
  Grid,
  Icon,
  SidePanel,
  SidePanelAccessibleTitle,
  Stack,
  Text,
} from '@orbit/hades';
import { MCP_CLIENTS, buildMcpConfig, isCommandClient, type McpClient } from '@orbit/lib/mcp';
import type { McpServer } from '../_types';
import { serverRollupScope } from '../_types';
import { ServerAvatar } from './server-avatar';
import { ConfigBlock } from './config-block';

// Hades caption/micro variants are leading-none; the spec composed these labels
// on the default 1.5 line box. `lineHeight` is kept inline where the line box
// height is load-bearing for the surrounding layout (stacked rows, chip rows).

interface BuildConfigPanelProps {
  servers: McpServer[];
  selected: string[];
  onClose: () => void;
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
        height="header"
        shrink
        style={{
          gap: 10,
          padding: '0 18px',
          borderBottom: '1px solid var(--color-border-subtle)',
        }}
      >
        <Icon name="terminal" color="accent" size="sm" />
        <Stack grow gap="none" style={{ minWidth: 0 }}>
          <Text as="span" size="supporting" weight="strong">
            Build config
          </Text>
          <Text as="span" size="micro" color="subtle" style={{ lineHeight: 1.5 }}>
            {chosen.length} server{chosen.length === 1 ? '' : 's'} · one gateway endpoint
          </Text>
        </Stack>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close panel">
          <Icon name="close" size="sm" />
        </Button>
      </Stack>

      {/* Body */}
      <Stack grow overflowY="auto" gap="comfortable" style={{ padding: 18 }}>
        {/* CLIENT */}
        <Stack gap="snug">
          <Eyebrow>Client</Eyebrow>
          <Grid cols={2} style={{ gap: 6 }}>
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
                    // Spec tiles select with a neutral border + overlay fill and sit on a
                    // transparent ghost base. Card selected / RadioCard both draw an accent
                    // border (and RadioCard adds a radio indicator), so either swap would
                    // change pixels — the pick-one state stays token-styled on Button.
                    border: `1px solid ${active ? 'var(--color-border)' : 'transparent'}`,
                    background: active ? 'var(--color-surface-overlay)' : 'transparent',
                    color: active ? 'var(--color-foreground)' : 'var(--color-subtle)',
                  }}
                >
                  <Icon name={c.icon} size="xs" />
                  <Text
                    as="span"
                    size="caption"
                    weight={active ? 'strong' : 'emphasis'}
                    color={active ? 'foreground' : 'subtle'}
                  >
                    {c.label}
                  </Text>
                </Button>
              );
            })}
          </Grid>
        </Stack>

        {/* CONFIG */}
        <Stack style={{ gap: 7 }}>
          <Stack direction="horizontal" align="center" justify="between">
            <Eyebrow>Config</Eyebrow>
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
            <Text as="span" size="caption" weight="strong" style={{ lineHeight: 1.5 }}>
              Authentication
            </Text>
            <Text as="span" size="caption" color="muted" style={{ lineHeight: 1.5 }}>
              Humans authorise via OAuth on first connect. A browser window opens. Agents receive a
              just-in-time injected token; no secret is stored in this file.
            </Text>
          </Stack>
        </Stack>

        {/* GRANTS REQUIRED */}
        <Stack gap="snug">
          <Eyebrow>Grants required</Eyebrow>
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
                  size="caption"
                  weight="emphasis"
                  color="muted"
                  style={{ flex: 1 }}
                >
                  {serverRollupScope(s.name)}
                </Text>
                <Text as="span" size="micro" color="subtle">
                  {s.caller_identity === 'forward' ? 'Delegated' : 'Service'}
                </Text>
              </Stack>
            ))}
          </Stack>
          <Text size="micro" color="subtle" style={{ margin: '2px 2px 0', lineHeight: 1.5 }}>
            These grants are requested when you connect. Anything outside them is filtered from{' '}
            <Text as="span" mono size="micro" color="subtle">
              tools/list
            </Text>{' '}
            and rejected at call time.
          </Text>
        </Stack>
      </Stack>

      {/* Footer */}
      <Stack
        shrink
        style={{
          padding: '14px 18px',
          borderTop: '1px solid var(--color-border-subtle)',
        }}
      >
        <CopyButton key={config} value={config} label="Copy config" />
      </Stack>
    </SidePanel>
  );
}
