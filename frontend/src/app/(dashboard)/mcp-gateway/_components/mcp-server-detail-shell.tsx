'use client';

/** @tier 3 — mcp-gateway-scoped. Client boundary for one server's tool view. */

import { McpServerDetailPage } from './mcp-server-detail-page';
import type { McpToolRow } from '../_types';

interface McpServerDetailShellProps {
  serverSlug: string;
  initialRows: McpToolRow[];
  initialError: string | null;
}

export function McpServerDetailShell({
  serverSlug,
  initialRows,
  initialError,
}: McpServerDetailShellProps) {
  return <McpServerDetailPage serverSlug={serverSlug} rows={initialRows} error={initialError} />;
}
