'use client';

/** @tier 3 — agents/[slug]-scoped. Read-only views of what the agent holds, drawn
 * from the agent's own definition so they do not depend on what the viewer can
 * use: no pickers, no checkboxes, no catalogue lookups. */

import { Badge, Card, CardContent, Stack, Text } from '@runkora/hades';
import type { Connector, ModelObject } from '@runkora/shared/api';

/** Splits `mcp:<server>:<tool>:use` into its server and tool. */
export function parseMcpScope(scope: string): { server: string; tool: string } {
  const body = scope.replace(/^mcp:/, '').replace(/:use$/, '');
  const colon = body.indexOf(':');
  if (colon < 0) return { server: body, tool: '' };
  return { server: body.slice(0, colon), tool: body.slice(colon + 1) };
}

/** Groups granted MCP scopes by server, both sorted. */
export function groupMcpScopes(scopes: string[]): { server: string; tools: string[] }[] {
  const byServer = new Map<string, string[]>();
  for (const scope of scopes) {
    const { server, tool } = parseMcpScope(scope);
    byServer.set(server, [...(byServer.get(server) ?? []), tool].filter(Boolean));
  }
  return [...byServer.entries()]
    .map(([server, tools]) => ({ server, tools: tools.sort() }))
    .sort((a, b) => a.server.localeCompare(b.server));
}

export function ReadOnlyModel({ modelId, models }: { modelId: string; models: ModelObject[] }) {
  if (!modelId) {
    return (
      <Text size="supporting" color="muted">
        No model set. The agent uses the platform default.
      </Text>
    );
  }
  // The catalogue only names models the viewer may use, so it can be absent.
  const displayName = models.find((m) => m.id === modelId)?.display_name;
  return (
    <Card>
      <CardContent padded>
        <Stack gap="tight">
          {displayName && (
            <Text as="span" size="supporting" weight="heavy">
              {displayName}
            </Text>
          )}
          <Text as="span" size="caption" color="muted" mono>
            {modelId}
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}

function HeldRow({ name, items }: { name: string; items: string[] }) {
  return (
    <Card>
      <CardContent padded>
        <Stack gap="cozy">
          <Text as="span" size="supporting" weight="heavy">
            {name}
          </Text>
          <Stack direction="horizontal" gap="snug" wrap>
            {items.length === 0 ? (
              <Text as="span" size="caption" color="muted">
                No tools or actions
              </Text>
            ) : (
              items.map((item) => (
                <Badge key={item} variant="outline">
                  {item}
                </Badge>
              ))
            )}
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export function ReadOnlyMcpTools({ scopes }: { scopes: string[] }) {
  const servers = groupMcpScopes(scopes);
  if (servers.length === 0) {
    return (
      <Text size="supporting" color="muted">
        This agent holds no MCP tools.
      </Text>
    );
  }
  return (
    <Stack gap="snug">
      {servers.map((s) => (
        <HeldRow key={s.server} name={s.server} items={s.tools} />
      ))}
    </Stack>
  );
}

export function ReadOnlyConnections({ connectors }: { connectors: Connector[] }) {
  if (connectors.length === 0) {
    return (
      <Text size="supporting" color="muted">
        This agent holds no connections.
      </Text>
    );
  }
  return (
    <Stack gap="snug">
      {connectors.map((c) => (
        <HeldRow key={c.connection} name={c.connection} items={c.actions} />
      ))}
    </Stack>
  );
}
