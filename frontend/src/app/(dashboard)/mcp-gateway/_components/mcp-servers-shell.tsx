'use client';

/** @tier 3 — mcp-gateway-scoped. Client boundary for the server list. */

import { McpServersPage } from './mcp-servers-page';
import type { McpServer } from '../_types';

interface McpServersShellProps {
  servers: McpServer[];
  error: string | null;
}

export function McpServersShell({ servers, error }: McpServersShellProps) {
  return <McpServersPage servers={servers} error={error} />;
}
