'use client';

/** @tier 3 — mcp-gateway-scoped. Read-only UI for the /mcp-gateway catalog page. */

import { useMemo } from 'react';
import { Badge, DataTable, Heading, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { distinctServers } from '../_types';
import type { McpToolRow } from '../_types';

interface McpGatewayPageProps {
  rows: McpToolRow[];
  error: string | null;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function toolMatchesSearch(row: McpToolRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.serverSlug.toLowerCase().includes(lower) ||
    row.toolName.toLowerCase().includes(lower) ||
    row.scope.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower)
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function McpGatewayPage({ rows, error }: McpGatewayPageProps) {
  const columns = useMemo<DataTableColumn<McpToolRow>[]>(
    () => [
      {
        id: 'server',
        header: 'Server',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.serverSlug,
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" align="center">
            <Badge variant="secondary" size="sm">
              {row.serverSlug}
            </Badge>
            {row.global ? (
              <Badge variant="outline" size="sm">
                global
              </Badge>
            ) : null}
          </Stack>
        ),
      },
      {
        id: 'tool',
        header: 'Tool',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.toolName,
        cell: (row) => (
          <Text mono size="supporting" weight="emphasis">
            {row.toolName}
          </Text>
        ),
      },
      {
        id: 'scope',
        header: 'Required scope',
        locked: true,
        cell: (row) => (
          <Badge variant="elevated" size="sm" mono>
            {row.scope}
          </Badge>
        ),
      },
      {
        id: 'description',
        header: 'Description',
        locked: true,
        cell: (row) =>
          row.description ? (
            <Text size="supporting" color="muted">
              {row.description}
            </Text>
          ) : (
            <Text size="supporting" color="muted">
              —
            </Text>
          ),
      },
    ],
    [],
  );

  const serverCount = distinctServers(rows);

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack gap="tight">
        <Heading variant="title">MCP Servers</Heading>
        <Text size="supporting" color="muted">
          Tools exposed by the MCP servers Orbit governs. Each tool is gated by its{' '}
          <Text as="span" mono size="supporting">
            mcp:&lt;server&gt;:&lt;tool&gt;:use
          </Text>{' '}
          scope — grant that scope to a principal (or an agent) to allow the tool through the
          gateway. Servers are registered as MCPServer custom resources (GitOps); this catalog is
          read-only.
        </Text>
        {rows.length > 0 ? (
          <Text size="supporting" color="muted">
            {rows.length} tool{rows.length === 1 ? '' : 's'} across {serverCount} server
            {serverCount === 1 ? '' : 's'}.
          </Text>
        ) : null}
      </Stack>

      {error ? (
        <Stack align="center" gap="snug" paddingBlock="comfortable">
          <Text size="supporting" color="muted">
            {error}
          </Text>
        </Stack>
      ) : rows.length === 0 ? (
        <EmptyState
          icon="server"
          title="No MCP tools"
          description="MCP servers are registered as MCPServer custom resources via GitOps and introspected by the tool-gateway. None are visible for this tenant yet."
        />
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(row) => row.scope}
          noun="tools"
          searchPlaceholder="Search tools, servers, scopes…"
          searchFn={toolMatchesSearch}
          defaultSortId="server"
          defaultSortDir="asc"
          emptyMessage="No MCP tools."
          filteredEmptyMessage="No tools match your search."
        />
      )}
    </Stack>
  );
}
