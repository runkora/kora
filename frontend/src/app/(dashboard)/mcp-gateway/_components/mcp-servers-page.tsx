'use client';

/** @tier 3 — mcp-gateway-scoped. MCP server catalog: cards / table, multi-select, config builder. */

import { useCallback, useMemo, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Checkbox,
  EmptyState,
  Grid,
  PageSectionHeader,
  Icon,
  Input,
  SegmentedControl,
  Show,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import { ClampedList, FilterChip, SmartTable, ViewToggle } from '@runkora/components';
import type { DataTableColumn, ViewMode } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { PLATFORM_MCP_SERVER } from '@runkora/lib/mcp';
import type { McpServer } from '../_types';
import { relativeTime, serverRollupScope } from '../_types';
import { ServerAvatar } from './server-avatar';
import { BuildConfigPanel } from './build-config-panel';

type Filter = 'all' | 'global' | 'tenant';

interface McpServersPageProps {
  servers: McpServer[];
  error: string | null;
}

const FILTER_LABELS: Record<Filter, string> = { all: 'All', global: 'Global', tenant: 'Tenant' };

function matchesQuery(s: McpServer, q: string): boolean {
  return (
    s.display_name.toLowerCase().includes(q) ||
    s.name.toLowerCase().includes(q) ||
    (s.description ?? '').toLowerCase().includes(q)
  );
}

function HealthDot({ status }: { status: string }) {
  const dot = status === 'healthy' ? 'healthy' : status === 'stale' ? 'degraded' : 'idle';
  return <StatusDot status={dot} size="sm" role="img" aria-label={`Status: ${status}`} />;
}

function MetaBadge({ icon, children }: { icon: IconName; children: React.ReactNode }) {
  return (
    <Badge variant="secondary" size="sm">
      <Icon name={icon} size="2xs" />
      {children}
    </Badge>
  );
}

function ScopeBadge({ global }: { global: boolean }) {
  return <MetaBadge icon={global ? 'globe' : 'building'}>{global ? 'Global' : 'Tenant'}</MetaBadge>;
}

function IdentityBadge({ delegated }: { delegated: boolean }) {
  return (
    <MetaBadge icon={delegated ? 'user' : 'box'}>{delegated ? 'Delegated' : 'Service'}</MetaBadge>
  );
}

function CapabilityList({ domains, maxRows }: { domains: string[]; maxRows?: number }) {
  return (
    <ClampedList
      items={domains}
      getKey={(d) => d}
      noun="capabilities"
      {...(maxRows === undefined ? { max: 3 } : { maxRows })}
      renderItem={(d) => (
        <Badge variant="elevated" size="sm" mono>
          {d}
        </Badge>
      )}
    />
  );
}

export function McpServersPage({ servers, error }: McpServersPageProps) {
  const router = useRouter();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [viewMode, setViewMode] = useState<ViewMode>('cards');
  const [filter, setFilter] = useState<Filter>('all');
  const [query, setQuery] = useState('');
  const [panelOpen, setPanelOpen] = useState(false);

  // The scope filter applies to both layouts; the text query is owned by the
  // table itself in list mode.
  const scoped = useMemo(
    () =>
      servers.filter((s) =>
        filter === 'global' ? s.global : filter === 'tenant' ? !s.global : true,
      ),
    [servers, filter],
  );
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? scoped.filter((s) => matchesQuery(s, q)) : scoped;
  }, [scoped, query]);

  const open = useCallback(
    (name: string) => router.push(APP_ROUTES.mcpGateway.server(name)),
    [router],
  );
  const toggle = useCallback(
    (name: string) =>
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        return next;
      }),
    [],
  );
  const selectedList = servers.filter((s) => selected.has(s.name));

  const filterCounts: Record<Filter, number> = {
    all: servers.length,
    global: servers.filter((s) => s.global).length,
    tenant: servers.filter((s) => !s.global).length,
  };
  const filters = Object.keys(FILTER_LABELS) as Filter[];

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <PageSectionHeader
          title="MCP servers"
          description="Select the servers you need and copy the generated config into your client."
          action={
            <Button size="sm" disabled={selected.size === 0} onClick={() => setPanelOpen(true)}>
              <Icon name="terminal" />
              Build config{selected.size > 0 ? ` · ${selected.size}` : ''}
            </Button>
          }
        />
        <Text size="supporting" color="muted">
          Connect to every MCP server through one governed endpoint.
        </Text>
      </Stack>

      {error ? (
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Text size="caption" color="status-error">
            {error}
          </Text>
        </Alert>
      ) : servers.length === 0 ? (
        <EmptyState
          icon="server"
          title="No MCP servers"
          description="MCP servers are registered as MCPServer custom resources via GitOps and introspected by the tool-gateway. None are visible for this tenant yet."
        />
      ) : viewMode === 'cards' ? (
        <>
          <Stack direction="horizontal" align="center" justify="between">
            <Stack asChild grow maxWidth="compact">
              <Input
                placeholder="Search servers…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </Stack>
            <ViewToggle viewMode={viewMode} onToggle={setViewMode} />
          </Stack>

          <Stack direction="horizontal" align="center" justify="between">
            <Stack direction="horizontal" align="center" gap="snug" wrap>
              {filters.map((f) => (
                <FilterChip
                  key={f}
                  label={FILTER_LABELS[f]}
                  count={filterCounts[f]}
                  active={filter === f}
                  onClick={() => setFilter(f)}
                />
              ))}
            </Stack>
            <Text size="caption" color="subtle">
              {filtered.length} server{filtered.length === 1 ? '' : 's'}
            </Text>
          </Stack>

          {filtered.length === 0 ? (
            <EmptyState
              icon="search"
              title="No matches"
              description="No servers match your search or filter. Try a different query."
            />
          ) : (
            <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
              {filtered.map((s) => (
                <ServerCard
                  key={s.name}
                  server={s}
                  selected={selected.has(s.name)}
                  onToggle={() => toggle(s.name)}
                />
              ))}
            </Grid>
          )}
        </>
      ) : (
        <ServerTable
          servers={scoped}
          filtered={filter !== 'all'}
          selected={selected}
          onToggle={toggle}
          onOpen={open}
          quickFilters={
            <SegmentedControl
              aria-label="Filter by access"
              value={filter}
              onChange={(v) => setFilter(v as Filter)}
              options={filters.map((f) => ({
                value: f,
                label: FILTER_LABELS[f],
                count: filterCounts[f],
              }))}
            />
          }
          toolbarActions={<ViewToggle viewMode={viewMode} onToggle={setViewMode} />}
        />
      )}

      {/* Room for the floating tray below the last card or row. */}
      {selected.size > 0 ? <Stack aria-hidden style={{ height: 56 }} /> : null}

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
            animation: 'mcp-tray-in 220ms cubic-bezier(0.16, 1, 0.3, 1)',
          }}
        >
          <Stack direction="horizontal" align="center" gap="comfortable" padding="snug">
            <Text size="supporting" weight="strong" truncate>
              {selected.size} selected
            </Text>
            <Show above="sm" display="inline">
              <Stack direction="horizontal" align="center" gap="tight">
                {selectedList.slice(0, 5).map((s) => (
                  <ServerAvatar key={s.name} name={s.display_name} size="xs" />
                ))}
              </Stack>
            </Show>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
              Clear
            </Button>
            <Button size="sm" onClick={() => setPanelOpen(true)}>
              Build config
              <Icon name="arrow-right" />
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

// ── Card view ────────────────────────────────────────────────────────────────

function ServerCard({
  server,
  selected,
  onToggle,
}: {
  server: McpServer;
  selected: boolean;
  onToggle: () => void;
}) {
  return (
    <Card column interactive selected={selected} selectedVariant="outline" onClick={onToggle}>
      <CardHeader compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <ServerAvatar name={server.display_name} accent={server.name === PLATFORM_MCP_SERVER} />
          <Stack grow gap="none">
            <Stack direction="horizontal" align="center" gap="snug">
              <Text weight="strong">{server.display_name}</Text>
              <HealthDot status={server.status} />
            </Stack>
            <Text mono size="caption" weight="emphasis" color="muted">
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
      </CardHeader>

      <CardContent grow>
        <Stack gap="cozy">
          <Text size="supporting" color="muted">
            {server.description || 'No description provided.'}
          </Text>
          <Stack
            direction="horizontal"
            wrap
            align="center"
            gap="tight"
            onClick={(e) => e.stopPropagation()}
          >
            <ScopeBadge global={server.global} />
            <IdentityBadge delegated={server.caller_identity === 'forward'} />
            <CapabilityList domains={server.domains} />
          </Stack>
        </Stack>
      </CardContent>

      <CardFooter compact justify="between">
        <Text size="caption" color="muted">
          <Text as="span" size="caption" weight="strong">
            {server.tool_count}
          </Text>{' '}
          tools · synced {relativeTime(server.last_synced)}
        </Text>
        <Button asChild variant="ghost" size="sm" onClick={(e) => e.stopPropagation()}>
          <Link href={APP_ROUTES.mcpGateway.server(server.name)}>
            Details
            <Icon name="chevron-right" size="2xs" />
          </Link>
        </Button>
      </CardFooter>
    </Card>
  );
}

// ── Table view ───────────────────────────────────────────────────────────────

function ServerTable({
  filtered,
  servers,
  selected,
  onToggle,
  onOpen,
  quickFilters,
  toolbarActions,
}: {
  servers: McpServer[];
  /** True when the access filter, not the data, may be why the table is empty. */
  filtered: boolean;
  selected: Set<string>;
  onToggle: (name: string) => void;
  onOpen: (name: string) => void;
  quickFilters: React.ReactNode;
  toolbarActions: React.ReactNode;
}) {
  const columns = useMemo<DataTableColumn<McpServer>[]>(
    () => [
      {
        id: 'select',
        header: '',
        gridWidth: '2.5rem',
        locked: true,
        cell: (s) => (
          <Stack onClick={(e) => e.stopPropagation()}>
            <Checkbox
              size="md"
              checked={selected.has(s.name)}
              onChange={() => onToggle(s.name)}
              aria-label={`Select ${s.display_name}`}
            />
          </Stack>
        ),
      },
      {
        id: 'server',
        header: 'Server',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '14rem',
        sortable: true,
        sortAccessor: (s) => s.display_name.toLowerCase(),
        cell: (s) => (
          <Stack direction="horizontal" align="center" gap="snug">
            <ServerAvatar name={s.display_name} accent={s.name === PLATFORM_MCP_SERVER} size="sm" />
            <Stack gap="none">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text weight="strong" size="supporting">
                  {s.display_name}
                </Text>
                <HealthDot status={s.status} />
              </Stack>
              <Text mono size="caption" color="muted" truncate>
                {serverRollupScope(s.name)}
              </Text>
            </Stack>
          </Stack>
        ),
      },
      {
        id: 'access',
        header: 'Access',
        gridWidth: '7rem',
        minWidth: '6rem',
        sortable: true,
        sortAccessor: (s) => (s.global ? 'global' : 'tenant'),
        cell: (s) => <ScopeBadge global={s.global} />,
      },
      {
        id: 'identity',
        header: 'Identity',
        gridWidth: '8rem',
        minWidth: '7rem',
        cell: (s) => <IdentityBadge delegated={s.caller_identity === 'forward'} />,
      },
      {
        id: 'capabilities',
        header: 'Capabilities',
        gridWidth: 'minmax(0, 3fr)',
        minWidth: '14rem',
        wrap: true,
        cell: (s) => <CapabilityList domains={s.domains} maxRows={2} />,
      },
      {
        id: 'tools',
        header: 'Tools',
        gridWidth: '5rem',
        minWidth: '4rem',
        sortable: true,
        sortAccessor: (s) => s.tool_count,
        cell: (s) => (
          <Text weight="strong" size="supporting">
            {s.tool_count}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '3rem',
        cell: (s) => (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Open ${s.display_name}`}
            onClick={(e) => {
              e.stopPropagation();
              onOpen(s.name);
            }}
          >
            <Icon name="chevron-right" size="2xs" />
          </Button>
        ),
      },
    ],
    [selected, onToggle, onOpen],
  );

  return (
    <SmartTable
      tableId="mcpServers"
      data={servers}
      columns={columns}
      getRowKey={(s) => s.name}
      noun="servers"
      searchPlaceholder="Search servers…"
      searchFn={matchesQuery}
      defaultSortId="server"
      defaultSortDir="asc"
      onRowClick={(s) => onOpen(s.name)}
      rowLabel={(s) => `Open ${s.display_name}`}
      emptyMessage={filtered ? 'No servers match this access filter.' : 'No MCP servers.'}
      filteredEmptyMessage="No servers match your search."
      quickFilters={quickFilters}
      toolbarActions={toolbarActions}
    />
  );
}
