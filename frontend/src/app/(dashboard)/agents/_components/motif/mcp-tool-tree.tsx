'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { useMemo, useState } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Checkbox,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@orbit/hades';
import type { McpCatalogTool, McpServer } from '@orbit/shared/api';

// ── Tree derivation (mirrors mcp-tools-fields grouping) ──────────────────────

interface ToolRow {
  name: string;
  scope: string;
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
  scopes: string[];
}

function buildTree(servers: McpServer[], catalog: McpCatalogTool[]): ServerRow[] {
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

/** How many servers have at least one granted scope — for callers' summary lines. */
export function countGrantedServers(
  servers: McpServer[],
  catalog: McpCatalogTool[],
  granted: string[],
): number {
  const grantedSet = new Set(granted);
  return buildTree(servers, catalog).filter((s) => s.scopes.some((sc) => grantedSet.has(sc)))
    .length;
}

export function McpToolTree({
  servers,
  catalog,
  granted,
  onChange,
}: {
  servers: McpServer[];
  catalog: McpCatalogTool[];
  granted: string[];
  onChange: (scopes: string[]) => void;
}) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const tree = useMemo(() => buildTree(servers, catalog), [servers, catalog]);
  const grantedSet = useMemo(() => new Set(granted), [granted]);

  const toggleMany = (scopes: string[], on: boolean) => {
    const next = new Set(grantedSet);
    for (const s of scopes) {
      if (on) next.add(s);
      else next.delete(s);
    }
    onChange([...next].sort());
  };

  return (
    <Stack gap="snug">
      {tree.map((server) => {
        const grantedHere = server.scopes.filter((sc) => grantedSet.has(sc));
        const on = grantedHere.length > 0;
        const whole = on && grantedHere.length === server.scopes.length;
        const isOpen = !!expanded[server.slug];
        return (
          <Card key={server.slug} selected={on} selectedVariant="outline">
            <Collapsible
              open={isOpen}
              onOpenChange={(next) => setExpanded((e) => ({ ...e, [server.slug]: next }))}
            >
              <CardContent flush>
                <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                  <Checkbox
                    size="md"
                    checked={whole}
                    indeterminate={on && !whole}
                    onChange={(checked) => toggleMany(server.scopes, checked)}
                    aria-label={`Grant all ${server.displayName} tools`}
                  />
                  <Icon
                    name="server"
                    size="sm"
                    color={on ? 'accent' : 'muted'}
                    container
                    containerVariant={on ? 'accent' : 'surface-subtle'}
                    label=""
                  />
                  <Stack grow gap="tight">
                    <Stack direction="horizontal" align="center" gap="snug">
                      <Text as="span" size="supporting" weight="heavy">
                        {server.displayName}
                      </Text>
                      <StatusDot size="sm" status="healthy" />
                    </Stack>
                    <Text as="span" size="micro" mono color="subtle">
                      mcp:{server.slug}:use
                    </Text>
                  </Stack>
                  <Text as="span" size="micro" weight="heavy" color={whole ? 'accent' : 'muted'}>
                    {on
                      ? whole
                        ? `All ${server.scopes.length} tools`
                        : `${grantedHere.length} of ${server.scopes.length} tools`
                      : ''}
                  </Text>
                  <CollapsibleTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`${isOpen ? 'Collapse' : 'Expand'} ${server.displayName} tools`}
                    >
                      <Icon name="chevron-down" rotate={isOpen ? 180 : 0} />
                    </Button>
                  </CollapsibleTrigger>
                </Stack>

                <CollapsibleContent>
                  <Separator />
                  <Stack direction="horizontal" align="center" gap="snug" padding="cozy">
                    <Text as="span" size="micro" color="subtle">
                      Choose the tools this agent may call
                    </Text>
                    <Stack grow />
                    <Button
                      variant="outline-muted"
                      size="xs"
                      onClick={() => toggleMany(server.scopes, true)}
                    >
                      Select all
                    </Button>
                    <Button
                      variant="outline-muted"
                      size="xs"
                      onClick={() => toggleMany(server.scopes, false)}
                    >
                      Clear
                    </Button>
                  </Stack>

                  {server.domains.map((domain) => {
                    const domainScopes = domain.tools.map((t) => t.scope);
                    const domainGranted = domainScopes.filter((sc) => grantedSet.has(sc));
                    const domainAll = domainGranted.length === domainScopes.length;
                    const domainSome = domainGranted.length > 0 && !domainAll;
                    return (
                      <Stack key={domain.domain} gap="none">
                        <Separator />
                        <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                          <Checkbox
                            checked={domainAll}
                            indeterminate={domainSome}
                            onChange={(checked) => toggleMany(domainScopes, checked)}
                            aria-label={`Grant all ${domain.domain} tools`}
                          />
                          <Text as="span" size="caption" weight="heavy">
                            {domain.domain}
                          </Text>
                          <Badge variant="outline" mono size="sm">
                            mcp:{server.slug}:{domain.domain}:use
                          </Badge>
                          <Stack grow />
                          <Text as="span" size="micro" mono color="subtle">
                            {domainGranted.length} / {domainScopes.length}
                          </Text>
                        </Stack>
                        {domain.tools.map((tool) => {
                          const toolOn = grantedSet.has(tool.scope);
                          return (
                            <Stack key={tool.scope} gap="none">
                              <Separator />
                              <Stack
                                direction="horizontal"
                                align="start"
                                gap="cozy"
                                padding="cozy"
                                paddingInline="relaxed"
                              >
                                <Checkbox
                                  checked={toolOn}
                                  onChange={(checked) => toggleMany([tool.scope], checked)}
                                  aria-label={`Grant ${tool.name}`}
                                />
                                <Stack
                                  gap="tight"
                                  minWidth="form-field"
                                  maxWidth="form-field"
                                  shrink={false}
                                >
                                  <Text as="span" size="caption" mono weight="heavy" truncate>
                                    {tool.name}
                                  </Text>
                                  <Text as="span" size="micro" mono color="subtle" truncate>
                                    {tool.scope}
                                  </Text>
                                </Stack>
                                <Text size="caption" color="muted">
                                  {tool.description}
                                </Text>
                              </Stack>
                            </Stack>
                          );
                        })}
                      </Stack>
                    );
                  })}
                </CollapsibleContent>
              </CardContent>
            </Collapsible>
          </Card>
        );
      })}
    </Stack>
  );
}
