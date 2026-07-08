'use client';

/** @tier 3 — mcp-gateway-scoped. MCP server catalog: rich cards / table, multi-select, config builder. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Checkbox, Grid, Heading, Icon, Input, Stack, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { McpServer } from '../_types';
import { relativeTime, serverRollupScope, statusColor } from '../_types';
import type { IconName } from '@orbit/terra';
import { ServerAvatar } from './server-avatar';
import { BuildConfigPanel } from './build-config-panel';

type Filter = 'all' | 'global' | 'tenant';
type View = 'grid' | 'table';

interface McpServersPageProps {
  servers: McpServer[];
  error: string | null;
}

function HealthDot({ status }: { status: string }) {
  return (
    <Text
      as="span"
      aria-hidden
      style={{
        width: 6,
        height: 6,
        borderRadius: 9999,
        background: statusColor(status),
        flexShrink: 0,
        display: 'inline-block',
      }}
    />
  );
}

/** A 22px pill matching the spec's metadata chips. `mono` = the outline domain style. */
function Chip({
  icon,
  mono = false,
  children,
}: {
  icon?: IconName;
  mono?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Text
      as="span"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: icon ? 5 : 0,
        height: 22,
        padding: '0 8px',
        borderRadius: 'var(--radius-sm)',
        border: '1px solid var(--color-border-subtle)',
        background: mono ? 'transparent' : 'var(--color-surface-overlay)',
        fontFamily: mono ? 'var(--font-mono)' : undefined,
        fontSize: 11,
        fontWeight: mono ? 500 : 400,
        lineHeight: 1,
        whiteSpace: 'nowrap',
        flexShrink: 0,
        color: mono ? 'var(--color-subtle)' : 'var(--color-muted)',
      }}
    >
      {icon ? <Icon name={icon} size="2xs" /> : null}
      {children}
    </Text>
  );
}

function ScopeChip({ global }: { global: boolean }) {
  return <Chip icon={global ? 'globe' : 'building'}>{global ? 'Global' : 'Tenant'}</Chip>;
}

function IdentityChip({ delegated }: { delegated: boolean }) {
  return <Chip icon={delegated ? 'user' : 'box'}>{delegated ? 'Delegated' : 'Service'}</Chip>;
}

/** The domain capability chips with a "+N" overflow past the first three. */
function CapabilityTags({ domains, max = 3 }: { domains: string[]; max?: number }) {
  const shown = domains.slice(0, max);
  const extra = domains.length - shown.length;
  return (
    <>
      {shown.map((d) => (
        <Chip key={d} mono>
          {d}
        </Chip>
      ))}
      {extra > 0 ? (
        <Text
          as="span"
          style={{
            height: 22,
            padding: '0 6px',
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: 11,
            color: 'var(--color-subtle)',
          }}
        >
          +{extra}
        </Text>
      ) : null}
    </>
  );
}

export function McpServersPage({ servers, error }: McpServersPageProps) {
  const router = useRouter();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [view, setView] = useState<View>('grid');
  const [filter, setFilter] = useState<Filter>('all');
  const [query, setQuery] = useState('');
  const [panelOpen, setPanelOpen] = useState(false);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return servers.filter((s) => {
      if (filter === 'global' && !s.global) return false;
      if (filter === 'tenant' && s.global) return false;
      if (!q) return true;
      return (
        s.display_name.toLowerCase().includes(q) ||
        s.name.toLowerCase().includes(q) ||
        (s.description ?? '').toLowerCase().includes(q)
      );
    });
  }, [servers, filter, query]);

  const open = (name: string) => router.push(APP_ROUTES.mcpGateway.server(name));
  const toggle = (name: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  const selectedList = servers.filter((s) => selected.has(s.name));

  return (
    <Stack gap="comfortable">
      {/* Intro + primary CTA */}
      <Stack direction="horizontal" justify="between" align="start" gap="comfortable" wrap>
        <Stack gap="tight" style={{ maxWidth: '46rem' }}>
          <Heading variant="title">MCP servers</Heading>
          <Text size="supporting" color="muted">
            One governed endpoint fronting every server. Tick the servers you need, then build a
            ready-to-paste config for your MCP client.
          </Text>
        </Stack>
        <Button
          variant="default"
          onClick={() => setPanelOpen(true)}
          disabled={selected.size === 0}
          style={{ height: 36, padding: '0 14px', gap: 8, fontSize: 13 }}
        >
          <Icon name="terminal" size="sm" />
          Build config{selected.size > 0 ? ` · ${selected.size}` : ''}
        </Button>
      </Stack>

      {error ? (
        <Text size="supporting" color="muted">
          {error}
        </Text>
      ) : servers.length === 0 ? (
        <EmptyState
          icon="server"
          title="No MCP servers"
          description="MCP servers are registered as MCPServer custom resources via GitOps and introspected by the tool-gateway. None are visible for this tenant yet."
        />
      ) : (
        <>
          {/* Controls: search · filter · count · view toggle */}
          <Stack direction="horizontal" justify="between" align="center" gap="comfortable" wrap>
            <Stack direction="horizontal" align="center" gap="snug" style={{ flex: '1 1 20rem' }}>
              <Stack
                direction="horizontal"
                align="center"
                gap="snug"
                paddingInline="snug"
                style={{
                  flex: '1 1 auto',
                  maxWidth: '22rem',
                  border: '1px solid var(--color-border)',
                  borderRadius: 'var(--radius-md)',
                }}
              >
                <Icon name="search" size="sm" color="muted" />
                <Input
                  placeholder="Search servers…"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  style={{ border: 'none', background: 'transparent', paddingInline: 0 }}
                />
              </Stack>

              <Stack
                direction="horizontal"
                gap="none"
                padding="tight"
                style={{
                  border: '1px solid var(--color-border)',
                  borderRadius: 'var(--radius-md)',
                }}
              >
                {(['all', 'global', 'tenant'] as Filter[]).map((f) => (
                  <Button
                    key={f}
                    variant={filter === f ? 'secondary' : 'ghost'}
                    size="sm"
                    onClick={() => setFilter(f)}
                  >
                    {f === 'all' ? 'All' : f === 'global' ? 'Global' : 'Tenant'}
                  </Button>
                ))}
              </Stack>
            </Stack>

            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="supporting" color="muted">
                {filtered.length} server{filtered.length === 1 ? '' : 's'}
              </Text>
              <Stack
                direction="horizontal"
                gap="none"
                padding="tight"
                style={{
                  border: '1px solid var(--color-border)',
                  borderRadius: 'var(--radius-md)',
                }}
              >
                <Button
                  variant={view === 'grid' ? 'secondary' : 'ghost'}
                  size="icon-sm"
                  aria-label="Grid view"
                  onClick={() => setView('grid')}
                >
                  <Icon name="layout-grid" size="sm" />
                </Button>
                <Button
                  variant={view === 'table' ? 'secondary' : 'ghost'}
                  size="icon-sm"
                  aria-label="Table view"
                  onClick={() => setView('table')}
                >
                  <Icon name="table" size="sm" />
                </Button>
              </Stack>
            </Stack>
          </Stack>

          {view === 'grid' ? (
            <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
              {filtered.map((s) => (
                <ServerCard
                  key={s.name}
                  server={s}
                  selected={selected.has(s.name)}
                  onToggle={() => toggle(s.name)}
                  onOpen={() => open(s.name)}
                />
              ))}
            </Grid>
          ) : (
            <ServerTable servers={filtered} selected={selected} onToggle={toggle} onOpen={open} />
          )}
        </>
      )}

      {/* Floating selection tray */}
      {selected.size > 0 ? (
        <Stack
          direction="horizontal"
          align="center"
          style={{
            position: 'fixed',
            bottom: 22,
            left: '50%',
            transform: 'translateX(-50%)',
            zIndex: 40,
            gap: 14,
            padding: '10px 12px 10px 18px',
            borderRadius: 'var(--radius-full)',
            border: '1px solid var(--color-border)',
            background: 'var(--color-surface-overlay)',
            boxShadow: '0 12px 32px oklch(0% 0 0 / 0.45)',
            animation: 'mcp-tray-in 220ms cubic-bezier(0.16, 1, 0.3, 1)',
          }}
        >
          <Text as="span" style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap' }}>
            <Text as="span" style={{ color: 'var(--color-accent)' }}>
              {selected.size}
            </Text>{' '}
            selected
          </Text>
          <Stack direction="horizontal" align="center">
            {selectedList.slice(0, 5).map((s, i) => (
              <Text
                as="span"
                key={s.name}
                title={s.display_name}
                style={{
                  width: 26,
                  height: 26,
                  marginLeft: i === 0 ? 0 : -6,
                  borderRadius: 'var(--radius-full)',
                  background: 'var(--color-surface)',
                  border: '1.5px solid var(--color-surface-overlay)',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontFamily: 'var(--font-mono)',
                  fontWeight: 700,
                  fontSize: 11,
                  color: 'var(--color-muted)',
                }}
              >
                {(s.name[0] ?? '?').toUpperCase()}
              </Text>
            ))}
          </Stack>
          <Button
            variant="ghost"
            shape="pill"
            onClick={() => setSelected(new Set())}
            style={{ height: 30, padding: '0 12px', fontSize: 12, color: 'var(--color-subtle)' }}
          >
            Clear
          </Button>
          <Button
            shape="pill"
            onClick={() => setPanelOpen(true)}
            style={{ height: 34, padding: '0 16px', gap: 7, fontSize: 13 }}
          >
            Build config
            <Icon name="arrow-right" size="xs" />
          </Button>
        </Stack>
      ) : null}

      {panelOpen && selectedList.length > 0 ? (
        <BuildConfigPanel
          servers={servers}
          selected={[...selected]}
          onClose={() => setPanelOpen(false)}
        />
      ) : null}
    </Stack>
  );
}

function ServerCard({
  server,
  selected,
  onToggle,
  onOpen,
}: {
  server: McpServer;
  selected: boolean;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const [hover, setHover] = useState(false);
  return (
    <Stack
      gap="none"
      style={{
        border: `1px solid ${
          selected || hover ? 'var(--color-accent)' : 'var(--color-border-subtle)'
        }`,
        background: selected ? 'var(--color-surface-elevated)' : 'var(--color-surface)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
        transition: 'border-color 150ms, background 150ms',
      }}
    >
      {/* Body — clicking toggles selection */}
      <Stack
        onClick={onToggle}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
        style={{
          padding: '16px 16px 14px',
          cursor: 'pointer',
          gap: 12,
          background: hover && !selected ? 'var(--color-surface-elevated)' : 'transparent',
          transition: 'background 150ms',
        }}
      >
        <Stack direction="horizontal" align="start" style={{ gap: 12 }}>
          <ServerAvatar name={server.name} accent={server.name === 'orbit'} />
          <Stack style={{ flex: 1, minWidth: 0, gap: 0 }}>
            <Stack direction="horizontal" align="center" style={{ gap: 8 }}>
              <Text as="span" style={{ fontSize: 15, fontWeight: 600, letterSpacing: '-0.01em' }}>
                {server.display_name}
              </Text>
              <HealthDot status={server.status} />
            </Stack>
            <Text
              as="span"
              style={{
                fontFamily: 'var(--font-mono)',
                fontWeight: 500,
                fontSize: 12,
                color: 'var(--color-subtle)',
                marginTop: 1,
              }}
            >
              {serverRollupScope(server.name)}
            </Text>
          </Stack>
          <Stack onClick={(e) => e.stopPropagation()}>
            <Checkbox
              size="lg"
              checked={selected}
              onChange={onToggle}
              aria-label={`Select ${server.display_name}`}
            />
          </Stack>
        </Stack>

        <Text
          style={{
            display: 'block',
            margin: 0,
            fontSize: 13,
            lineHeight: 1.5,
            color: 'var(--color-muted)',
            minHeight: 38,
          }}
        >
          {server.description || 'No description provided.'}
        </Text>

        <Stack direction="horizontal" wrap align="center" style={{ gap: 6 }}>
          <ScopeChip global={server.global} />
          <IdentityChip delegated={server.caller_identity === 'forward'} />
          <CapabilityTags domains={server.domains} />
        </Stack>
      </Stack>

      {/* Footer */}
      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        style={{ padding: '10px 16px', borderTop: '1px solid var(--color-border-subtle)' }}
      >
        <Text as="span" style={{ fontSize: 12, color: 'var(--color-subtle)' }}>
          <Text as="span" style={{ fontSize: 12, color: 'var(--color-muted)', fontWeight: 600 }}>
            {server.tool_count}
          </Text>{' '}
          tools · synced {relativeTime(server.last_synced)}
        </Text>
        <Button
          variant="ghost"
          size="sm"
          onClick={(e) => {
            e.stopPropagation();
            onOpen();
          }}
          style={{ height: 26, padding: '0 8px' }}
        >
          Details
          <Icon name="chevron-right" size="2xs" />
        </Button>
      </Stack>
    </Stack>
  );
}

const TABLE_COLS = '36px minmax(220px, 1fr) 116px 124px minmax(160px, 240px) 60px 40px';

function ServerTable({
  servers,
  selected,
  onToggle,
  onOpen,
}: {
  servers: McpServer[];
  selected: Set<string>;
  onToggle: (name: string) => void;
  onOpen: (name: string) => void;
}) {
  const [hoveredRow, setHoveredRow] = useState<string | null>(null);
  return (
    <Stack
      gap="none"
      style={{
        border: '1px solid var(--color-border)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <Stack
        gap="none"
        paddingInline="comfortable"
        paddingBlock="snug"
        style={{
          display: 'grid',
          gridTemplateColumns: TABLE_COLS,
          columnGap: 14,
          alignItems: 'center',
          background: 'var(--color-surface-elevated)',
        }}
      >
        <Stack />
        <ColLabel>Server</ColLabel>
        <ColLabel>Access</ColLabel>
        <ColLabel>Identity</ColLabel>
        <ColLabel>Capabilities</ColLabel>
        <ColLabel>Tools</ColLabel>
        <Stack />
      </Stack>

      {servers.map((s, i) => {
        const isSel = selected.has(s.name);
        return (
          <Stack
            key={s.name}
            paddingInline="comfortable"
            paddingBlock="snug"
            onClick={() => onOpen(s.name)}
            onMouseEnter={() => setHoveredRow(s.name)}
            onMouseLeave={() => setHoveredRow(null)}
            style={{
              display: 'grid',
              gridTemplateColumns: TABLE_COLS,
              columnGap: 14,
              alignItems: 'center',
              cursor: 'pointer',
              borderTop: i === 0 ? 'none' : '1px solid var(--color-border)',
              transition: 'background 150ms',
              ...(isSel
                ? { background: 'color-mix(in oklch, var(--color-accent) 6%, transparent)' }
                : hoveredRow === s.name
                  ? { background: 'var(--color-surface-elevated)' }
                  : {}),
            }}
          >
            <Stack onClick={(e) => e.stopPropagation()}>
              <Checkbox
                size="md"
                checked={isSel}
                onChange={() => onToggle(s.name)}
                aria-label={`Select ${s.display_name}`}
              />
            </Stack>
            <Stack direction="horizontal" align="center" gap="snug">
              <ServerAvatar name={s.name} accent={s.name === 'orbit'} size="sm" />
              <Stack gap="none">
                <Stack direction="horizontal" align="center" gap="snug">
                  <Text weight="heavy" size="supporting">
                    {s.display_name}
                  </Text>
                  <HealthDot status={s.status} />
                </Stack>
                <Text mono size="caption" color="muted">
                  {serverRollupScope(s.name)}
                </Text>
              </Stack>
            </Stack>
            <Stack direction="horizontal">
              <ScopeChip global={s.global} />
            </Stack>
            <Stack direction="horizontal">
              <IdentityChip delegated={s.caller_identity === 'forward'} />
            </Stack>
            <Stack
              direction="horizontal"
              align="center"
              style={{ gap: 6, minWidth: 0, overflow: 'hidden' }}
            >
              <CapabilityTags domains={s.domains} />
            </Stack>
            <Text weight="heavy" size="supporting">
              {s.tool_count}
            </Text>
            <Icon name="chevron-right" size="2xs" color="muted" />
          </Stack>
        );
      })}
    </Stack>
  );
}

function ColLabel({ children }: { children: string }) {
  return (
    <Text
      as="span"
      style={{
        fontSize: 10,
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.1em',
        color: 'var(--color-subtle)',
      }}
    >
      {children}
    </Text>
  );
}
