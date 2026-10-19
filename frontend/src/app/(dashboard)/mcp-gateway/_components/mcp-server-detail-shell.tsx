'use client';

/** @tier 3 — mcp-gateway-scoped. Client boundary for one server's detail view. */

import type { McpCatalogTool } from '@runkora/shared/api';
import { McpServerDetailPage } from './mcp-server-detail-page';
import type { McpServer } from '../_types';

interface McpServerDetailShellProps {
  serverSlug: string;
  server: McpServer | null;
  tools: McpCatalogTool[];
  error: string | null;
}

export function McpServerDetailShell({
  serverSlug,
  server,
  tools,
  error,
}: McpServerDetailShellProps) {
  return (
    <McpServerDetailPage serverSlug={serverSlug} server={server} tools={tools} error={error} />
  );
}
