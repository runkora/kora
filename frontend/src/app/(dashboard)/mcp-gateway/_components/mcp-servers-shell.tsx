'use client';

/** @tier 3 — mcp-gateway-scoped. Client boundary for the server list. */

import { McpServersPage } from './mcp-servers-page';
import type { McpToolRow } from '../_types';

interface McpServersShellProps {
  initialRows: McpToolRow[];
  initialError: string | null;
}

export function McpServersShell({ initialRows, initialError }: McpServersShellProps) {
  return <McpServersPage rows={initialRows} error={initialError} />;
}
