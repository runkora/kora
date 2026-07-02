/**
 * MCP tool-gateway client helpers.
 *
 * NOTE: MCP_GATEWAY_HOST is a PLACEHOLDER. Orbit does not yet expose a public
 * MCP gateway ingress — that's the subject of a forthcoming "MCP Gateway Public
 * Exposure" ADR. Once it lands, this becomes a per-environment config value.
 */
import type { IconName } from '@orbit/terra';

export const MCP_GATEWAY_HOST = 'gw.orbit.sh';

/** The public URL an MCP client uses to reach one server through the gateway. */
export function mcpServerUrl(serverSlug: string): string {
  return `https://${MCP_GATEWAY_HOST}/mcp/${serverSlug}`;
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
