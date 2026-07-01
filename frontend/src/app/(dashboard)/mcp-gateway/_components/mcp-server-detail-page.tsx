'use client';

/** @tier 3 — mcp-gateway-scoped. One server's tools + the scope each requires. */

import { useMemo } from 'react';
import Link from 'next/link';
import { Badge, Button, DataTable, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { toolsForServer } from '../_types';
import type { McpToolRow } from '../_types';

interface McpServerDetailPageProps {
  serverSlug: string;
  rows: McpToolRow[];
  error: string | null;
}

function toolMatchesSearch(row: McpToolRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.toolName.toLowerCase().includes(lower) ||
    row.scope.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower)
  );
}

export function McpServerDetailPage({ serverSlug, rows, error }: McpServerDetailPageProps) {
  const tools = useMemo(() => toolsForServer(rows, serverSlug), [rows, serverSlug]);
  const isGlobal = tools[0]?.global ?? false;

  const columns = useMemo<DataTableColumn<McpToolRow>[]>(
    () => [
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
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.description || '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Button variant="ghost" size="sm" asChild>
          <Link href={APP_ROUTES.mcpGateway.servers}>
            <Stack direction="horizontal" gap="tight" align="center">
              <Icon name="arrow-left" size="2xs" />
              <Text size="supporting">All MCP servers</Text>
            </Stack>
          </Link>
        </Button>
        <Stack direction="horizontal" gap="snug" align="center">
          <Heading variant="title">{serverSlug}</Heading>
          <Badge variant={isGlobal ? 'secondary' : 'outline'} size="sm">
            {isGlobal ? 'global' : 'tenant'}
          </Badge>
        </Stack>
        <Text size="supporting" color="muted">
          {tools.length} tool{tools.length === 1 ? '' : 's'}. Grant a tool&apos;s scope to a
          principal (or an agent) to allow it through the gateway.
        </Text>
      </Stack>

      {error ? (
        <Stack align="center" gap="snug" paddingBlock="comfortable">
          <Text size="supporting" color="muted">
            {error}
          </Text>
        </Stack>
      ) : tools.length === 0 ? (
        <EmptyState
          icon="server"
          title="No tools"
          description="This server exposes no tools, or it is not visible for this tenant."
        />
      ) : (
        <DataTable
          data={tools}
          columns={columns}
          getRowKey={(row) => row.scope}
          noun="tools"
          searchPlaceholder="Search tools, scopes…"
          searchFn={toolMatchesSearch}
          defaultSortId="tool"
          defaultSortDir="asc"
          emptyMessage="No tools."
          filteredEmptyMessage="No tools match your search."
        />
      )}
    </Stack>
  );
}
