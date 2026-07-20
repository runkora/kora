'use client';

/** @tier 3 — agents-scoped. Governed MCP tool grants: pick the exact tools an
 * agent may call through the tool-gateway. Fine-grained — per tool, with
 * per-server and per-domain "select all" rollups. Compiles to
 * mcp:<server>:<tool>:use grant scopes (the credential parallel to
 * connectivity-fields). Shared by the create form and the
 * connectivity tab. */

import { useMemo } from 'react';
import { Badge, Checkbox, Stack, Text } from '@orbit/terra';
import type { McpCatalogTool, McpServer } from '@orbit/shared/api';

interface McpToolsFieldsProps {
  /** Servers visible to the tenant (GET /v1/servers via the tool-gateway). */
  servers: McpServer[];
  /** The tool catalog (GET /v1/catalog): one row per tool with its scope. */
  catalog: McpCatalogTool[];
  /** Currently-granted mcp:<server>:<tool>:use scopes (a subset of grant_scopes). */
  value: string[];
  onChange: (scopes: string[]) => void;
}

interface ToolRow {
  name: string; // leaf, e.g. "agents.list"
  scope: string; // mcp:<server>:<tool>:use
  description: string;
}
interface DomainRow {
  domain: string;
  tools: ToolRow[];
}
interface ServerRow {
  slug: string;
  displayName: string;
  domains: DomainRow[];
  scopes: string[]; // every tool scope under this server
}

function group(servers: McpServer[], catalog: McpCatalogTool[]): ServerRow[] {
  const bySlug = new Map<string, ServerRow>();
  const displayName = (slug: string) => servers.find((s) => s.name === slug)?.display_name ?? slug;

  for (const t of catalog) {
    let server = bySlug.get(t.server_slug);
    if (!server) {
      server = {
        slug: t.server_slug,
        displayName: displayName(t.server_slug),
        domains: [],
        scopes: [],
      };
      bySlug.set(t.server_slug, server);
    }
    server.scopes.push(t.scope);
    const dot = t.tool_name.indexOf('.');
    const domainName = dot >= 0 ? t.tool_name.slice(0, dot) : t.tool_name;
    let domain = server.domains.find((d) => d.domain === domainName);
    if (!domain) {
      domain = { domain: domainName, tools: [] };
      server.domains.push(domain);
    }
    domain.tools.push({ name: t.tool_name, scope: t.scope, description: t.description });
  }

  const rows = [...bySlug.values()];
  for (const s of rows) {
    s.domains.sort((a, b) => a.domain.localeCompare(b.domain));
    for (const d of s.domains) d.tools.sort((a, b) => a.name.localeCompare(b.name));
  }
  return rows.sort((a, b) => a.slug.localeCompare(b.slug));
}

/** Tri-state: all / some / none of `scopes` are in the granted set. */
function coverage(granted: Set<string>, scopes: string[]): 'all' | 'some' | 'none' {
  let on = 0;
  for (const s of scopes) if (granted.has(s)) on++;
  if (on === 0) return 'none';
  return on === scopes.length ? 'all' : 'some';
}

export function McpToolsFields({ servers, catalog, value, onChange }: McpToolsFieldsProps) {
  const rows = useMemo(() => group(servers, catalog), [servers, catalog]);
  const granted = useMemo(() => new Set(value), [value]);

  const setScopes = (scopes: string[], on: boolean) => {
    const next = new Set(granted);
    for (const s of scopes) {
      if (on) next.add(s);
      else next.delete(s);
    }
    onChange([...next]);
  };

  if (rows.length === 0) {
    return (
      <Text size="supporting" color="muted">
        No MCP tools are available for this tenant. Register an MCPServer and let the tool-gateway
        introspect it to grant the agent governed tool access.
      </Text>
    );
  }

  return (
    <Stack gap="comfortable">
      {rows.map((server) => {
        const serverCov = coverage(granted, server.scopes);
        return (
          <Stack key={server.slug} gap="snug">
            {/* Server header with a whole-server select-all */}
            <Stack direction="horizontal" align="center" justify="between" gap="snug">
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="supporting" weight="emphasis">
                  {server.displayName}
                </Text>
                <Badge variant="dim" size="sm">
                  <Text as="span" mono size="caption">
                    mcp:{server.slug}
                  </Text>
                </Badge>
              </Stack>
              <Checkbox
                checked={serverCov === 'all'}
                indeterminate={serverCov === 'some'}
                onChange={(on) => setScopes(server.scopes, on)}
              >
                <Text size="caption" color="muted">
                  All tools
                </Text>
              </Checkbox>
            </Stack>

            {server.domains.map((domain) => {
              const domainScopes = domain.tools.map((t) => t.scope);
              const domainCov = coverage(granted, domainScopes);
              return (
                <Stack
                  key={domain.domain}
                  gap="tight"
                  style={{
                    padding: '10px 12px',
                    border: '1px solid var(--color-border-subtle)',
                    borderRadius: 'var(--radius-md)',
                  }}
                >
                  <Checkbox
                    checked={domainCov === 'all'}
                    indeterminate={domainCov === 'some'}
                    onChange={(on) => setScopes(domainScopes, on)}
                  >
                    <Text
                      size="supporting"
                      weight="emphasis"
                      style={{ textTransform: 'capitalize' }}
                    >
                      {domain.domain}
                    </Text>
                  </Checkbox>
                  <Stack gap="tight" style={{ paddingLeft: 26 }}>
                    {domain.tools.map((tool) => (
                      <Checkbox
                        key={tool.scope}
                        checked={granted.has(tool.scope)}
                        onChange={(on) => setScopes([tool.scope], on)}
                      >
                        <Stack gap="none">
                          <Text as="span" mono size="caption">
                            {tool.name}
                          </Text>
                          {tool.description ? (
                            <Text size="caption" color="muted">
                              {tool.description}
                            </Text>
                          ) : null}
                        </Stack>
                      </Checkbox>
                    ))}
                  </Stack>
                </Stack>
              );
            })}
          </Stack>
        );
      })}
    </Stack>
  );
}
