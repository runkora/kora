'use client';

/** @tier 3 — mcp-gateway-scoped. Client boundary holding the server-fetched rows. */

import { McpGatewayPage } from './mcp-gateway-page';
import type { McpToolRow } from '../_types';

interface McpGatewayShellProps {
  initialRows: McpToolRow[];
  initialError: string | null;
}

export function McpGatewayShell({ initialRows, initialError }: McpGatewayShellProps) {
  return <McpGatewayPage rows={initialRows} error={initialError} />;
}
