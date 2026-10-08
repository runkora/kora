/**
 * MCP tool-gateway client helpers.
 *
 * Per the north-south exposure convention (ADR 2026-07-04), backend APIs — the
 * MCP gateway included — live on the `api.` host, while the dashboard is on the
 * bare host. So we derive the gateway origin from the dashboard origin by
 * prefixing `api.` ("derive, don't hardcode"). Each server is mounted at its own
 * path: POST /v1/mcp/<server> (ADR 2026-06-29 Decision 5, per-server mounts) — a
 * client connects per server, not to one aggregate endpoint.
 */
import type { IconName } from '@orbit/hades';

/** The `api.` host an MCP client uses to reach the gateway, derived from the dashboard origin. */
export function mcpGatewayHost(): string {
  if (typeof window === 'undefined') return '';
  const host = window.location.host;
  return host.startsWith('api.') ? host : `api.${host}`;
}

/** The gateway origin (scheme + api. host) for the current dashboard. */
function mcpGatewayOrigin(): string {
  if (typeof window === 'undefined') return '';
  return `${window.location.protocol}//${mcpGatewayHost()}`;
}

/** The URL an MCP client uses to reach one server through the gateway. */
export function mcpServerUrl(serverSlug: string): string {
  const origin = mcpGatewayOrigin();
  return origin ? `${origin}/v1/mcp/${serverSlug}` : '';
}

/** The MCP clients we generate connection config for. */
export type McpClient = 'desktop' | 'cursor' | 'code' | 'json';

export const MCP_CLIENTS: {
  id: McpClient;
  label: string;
  short: string;
  icon: IconName;
  file: string;
}[] = [
  {
    id: 'desktop',
    label: 'Claude Desktop',
    short: 'Desktop',
    icon: 'monitor',
    file: '~/Library/Application Support/Claude/claude_desktop_config.json',
  },
  { id: 'cursor', label: 'Cursor', short: 'Cursor', icon: 'code-2', file: '.cursor/mcp.json' },
  {
    id: 'code',
    label: 'Claude Code',
    short: 'Code',
    icon: 'terminal',
    file: 'run in your project terminal',
  },
  { id: 'json', label: 'Raw JSON', short: 'JSON', icon: 'braces', file: 'mcp.json' },
];

/** Whether a client's config is a shell command (Claude Code) vs a JSON block. */
export function isCommandClient(client: McpClient): boolean {
  return client === 'code';
}

/**
 * Builds the connection config for the given servers, adapting to the client:
 * Claude Code takes `claude mcp add` CLI commands; every other client takes the
 * mcpServers JSON block. One entry per server, each pointing at its gateway URL.
 */
export function buildMcpConfig(client: McpClient, serverSlugs: string[]): string {
  if (serverSlugs.length === 0) return '';
  if (isCommandClient(client)) {
    return serverSlugs
      .map((slug) => `claude mcp add --transport http ${slug} ${mcpServerUrl(slug)}`)
      .join('\n');
  }
  const servers: Record<string, { url: string; type: string }> = {};
  for (const slug of serverSlugs) {
    servers[slug] = { url: mcpServerUrl(slug), type: 'streamable-http' };
  }
  return JSON.stringify({ mcpServers: servers }, null, 2);
}
