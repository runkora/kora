'use client';

/** @tier 3 — mcp-gateway-scoped. MCP server catalog: rich cards / table, multi-select, config builder. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Button,
  Card,
  Checkbox,
  Eyebrow,
  Grid,
  Heading,
  Icon,
  Input,
  Stack,
  Text,
  EmptyState,
} from '@runkora/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { McpServer } from '../_types';
import { relativeTime, serverRollupScope, statusColor } from '../_types';
import type { IconName } from '@runkora/hades';
import { ServerAvatar } from './server-avatar';
import { BuildConfigPanel } from './build-config-panel';

// Hades caption/micro variants are leading-none; the spec composed some labels
// on the default 1.5 line box. `lineHeight` is kept inline where the line box
// height is load-bearing for the surrounding layout. Font sizes 13/15px sit
// more than a sub-pixel from any Text variant, so those spec values are kept.

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
        borderRadius: 'var(--radius-full)',
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
      size="micro"
      weight={mono ? 'emphasis' : 'regular'}
      color="muted"
      mono={mono}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: icon ? 5 : 0,
        height: 22,
        padding: '0 8px',
        borderRadius: 'var(--radius-sm)',
        border: '1px solid var(--color-border-subtle)',
        background: mono ? 'transparent' : 'var(--color-surface-overlay)',
        whiteSpace: 'nowrap',
        flexShrink: 0,
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
          size="micro"
          color="muted"
          style={{
            height: 22,
            padding: '0 6px',
            display: 'inline-flex',
            alignItems: 'center',
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
          // 13px sits between caption (12) and supporting (14); spec value kept.
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
        <Card
          border="default"
          subtle
          style={{
            position: 'fixed',
            bottom: 22,
            left: '50%',
            transform: 'translateX(-50%)',
            zIndex: 40,
            borderRadius: 'var(--radius-full)',
            // Card subtle = surface-elevated; the spec tray floats one step higher.
            background: 'var(--color-surface-overlay)',
            // Hades is flat (ADR SD-3, no shadow tokens), but the tray floats over
            // page content and needs a drop for legibility; expressed through the
            // overlay scrim token rather than a raw colour.
            boxShadow: '0 12px 32px var(--color-overlay)',
            animation: 'mcp-tray-in 220ms cubic-bezier(0.16, 1, 0.3, 1)',
          }}
        >
          <Stack
            direction="horizontal"
            align="center"
            style={{ gap: 14, padding: '10px 12px 10px 18px' }}
          >
            <Text as="span" weight="strong" style={{ fontSize: 13, whiteSpace: 'nowrap' }}>
              <Text as="span" color="accent">
                {selected.size}
              </Text>{' '}
              selected
            </Text>
            <Stack direction="horizontal" align="center">
              {/* Hand-rolled letter tiles — kept as-is (future AvatarTile candidate). */}
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
              style={{ height: 30, padding: '0 12px' }}
            >
              <Text as="span" size="caption" color="subtle">
                Clear
              </Text>
            </Button>
            <Button
              shape="pill"
              onClick={() => setPanelOpen(true)}
              // 13px sits between caption (12) and supporting (14); spec value kept.
              style={{ height: 34, padding: '0 16px', gap: 7, fontSize: 13 }}
            >
              Build config
              <Icon name="arrow-right" size="xs" />
            </Button>
          </Stack>
        </Card>
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
    <Card
      selected={selected}
      selectedVariant="outline"
      style={{
        // Spec draws the selected/hover border in accent-400; Card's selected
        // border token is accent-600, so the border colour is overridden with
        // the matching token to keep pixels identical.
        borderColor: selected || hover ? 'var(--color-accent)' : undefined,
        transition: 'border-color 150ms, background 150ms',
      }}
    >
      {/* Body — clicking toggles selection */}
      <Stack
        onClick={onToggle}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
        gap="cozy"
        style={{
          padding: '16px 16px 14px',
          cursor: 'pointer',
          background: hover && !selected ? 'var(--color-surface-elevated)' : 'transparent',
          transition: 'background 150ms',
        }}
      >
        <Stack direction="horizontal" align="start" gap="cozy">
          <ServerAvatar name={server.name} accent={server.name === 'orbit'} />
          <Stack grow gap="none" style={{ minWidth: 0 }}>
            <Stack direction="horizontal" align="center" gap="snug">
              <Text as="span" weight="strong" style={{ fontSize: 15, letterSpacing: '-0.01em' }}>
                {server.display_name}
              </Text>
              <HealthDot status={server.status} />
            </Stack>
            <Text
              as="span"
              mono
              size="caption"
              weight="emphasis"
              color="muted"
              style={{ marginTop: 1, lineHeight: 1.5 }}
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

        <Text color="muted" style={{ fontSize: 13, lineHeight: 1.5, minHeight: 38 }}>
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
        <Text as="span" size="caption" color="muted">
          <Text as="span" size="caption" weight="strong">
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
    </Card>
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
        <Eyebrow>Server</Eyebrow>
        <Eyebrow>Access</Eyebrow>
        <Eyebrow>Identity</Eyebrow>
        <Eyebrow>Capabilities</Eyebrow>
        <Eyebrow>Tools</Eyebrow>
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
              // Selected rows wash with a 6% accent tint (token-derived); Card's
              // selected surface doesn't apply to grid rows, so the mix is kept.
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
