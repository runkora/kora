'use client';

/** @tier 3 — mcp-gateway-scoped. Top-level list of MCP servers; drill into one for its tools. */

import { useMemo } from 'react';
import Link from 'next/link';
import { Badge, Button, DataTable, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { buildServerRows } from '../_types';
import type { McpToolRow } from '../_types';

interface McpServersPageProps {
  rows: McpToolRow[];
  error: string | null;
}

function serverMatchesSearch(row: { serverSlug: string }, q: string): boolean {
  return row.serverSlug.toLowerCase().includes(q.toLowerCase());
}

export function McpServersPage({ rows, error }: McpServersPageProps) {
  const servers = useMemo(() => buildServerRows(rows), [rows]);

  const columns = useMemo<DataTableColumn<(typeof servers)[number]>[]>(
    () => [
      {
        id: 'server',
        header: 'Server',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.serverSlug,
        cell: (row) => (
          <Button variant="ghost" size="sm" asChild>
            <Link href={APP_ROUTES.mcpGateway.server(row.serverSlug)}>
              <Text mono size="supporting" weight="emphasis">
                {row.serverSlug}
              </Text>
            </Link>
          </Button>
        ),
      },
      {
        id: 'scope',
        header: 'Scope',
        locked: true,
        cell: (row) => (
          <Badge variant={row.global ? 'secondary' : 'outline'} size="sm">
            {row.global ? 'global' : 'tenant'}
          </Badge>
        ),
      },
      {
        id: 'tools',
        header: 'Tools',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.toolCount,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.toolCount}
          </Text>
        ),
      },
      {
        id: 'go',
        header: '',
        locked: true,
        cell: (row) => (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`View ${row.serverSlug} tools`}
            asChild
          >
            <Link href={APP_ROUTES.mcpGateway.server(row.serverSlug)}>
              <Icon name="chevron-right" size="2xs" />
            </Link>
          </Button>
        ),
      },
    ],
    [],
  );

  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Heading variant="title">MCP Servers</Heading>
        <Text size="supporting" color="muted">
          The MCP servers Orbit governs. Each server&apos;s tools are gated by{' '}
          <Text as="span" mono size="supporting">
            mcp:&lt;server&gt;:&lt;tool&gt;:use
          </Text>{' '}
          scopes — open a server to see its tools and the scope each requires. Servers are
          registered as MCPServer custom resources (GitOps); this catalog is read-only.
        </Text>
      </Stack>

      {error ? (
        <Stack align="center" gap="snug" paddingBlock="comfortable">
          <Text size="supporting" color="muted">
            {error}
          </Text>
        </Stack>
      ) : servers.length === 0 ? (
        <EmptyState
          icon="server"
          title="No MCP servers"
          description="MCP servers are registered as MCPServer custom resources via GitOps and introspected by the tool-gateway. None are visible for this tenant yet."
        />
      ) : (
        <DataTable
          data={servers}
          columns={columns}
          getRowKey={(row) => row.serverSlug}
          noun="servers"
          searchPlaceholder="Search servers…"
          searchFn={serverMatchesSearch}
          defaultSortId="server"
          defaultSortDir="asc"
          emptyMessage="No MCP servers."
          filteredEmptyMessage="No servers match your search."
        />
      )}
    </Stack>
  );
}
