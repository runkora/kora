'use client';

/** @tier 3 — agents-scoped. Builder step 3: MCP tool tree, platform tools, connections, egress, environment. */

import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  Checkbox,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Grid,
  Icon,
  NativeSelect,
  RadioGroup,
  RadioGroupItem,
  Separator,
  Stack,
  StatusDot,
  Text,
  Textarea,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import type {
  ConnectivityMode,
  Connector,
  EffectiveAccess,
  McpCatalogTool,
  McpServer,
} from '@orbit/shared/api';
import { previewEffectiveAccess } from '../../[slug]/_services/agent-detail-api';
import { EffectiveAccessPanel } from '../../[slug]/_components/effective-access-panel';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { StepHeader } from './step-header';

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

/** Section heading shared by every block on this step. */
function SectionLabel({ title, desc }: { title: string; desc: string }) {
  return (
    <Stack gap="tight">
      <Text as="span" size="caption" weight="heavy" color="muted">
        {title}
      </Text>
      <Text size="micro" color="subtle">
        {desc}
      </Text>
    </Stack>
  );
}

const EGRESS_MODES: { id: ConnectivityMode; name: string; desc: string; icon: IconName }[] = [
  {
    id: 'none',
    name: 'None',
    desc: 'No outbound internet access. The agent can still reach platform services.',
    icon: 'lock',
  },
  {
    id: 'whitelist',
    name: 'Allowlist',
    desc: 'Outbound access only to the hosts you list.',
    icon: 'network',
  },
  {
    id: 'open',
    name: 'Open',
    desc: 'Any host. Credentialed destinations are still gated by connections.',
    icon: 'globe',
  },
];

export function StepTools() {
  const { fields, setField, catalogues } = useAgentBuilderContext();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const tree = useMemo(
    () => buildTree(catalogues.mcpServers, catalogues.mcpCatalog),
    [catalogues.mcpServers, catalogues.mcpCatalog],
  );
  const granted = useMemo(() => new Set(fields.mcpTools), [fields.mcpTools]);
  const grantedServers = tree.filter((s) => s.scopes.some((sc) => granted.has(sc)));

  const setScopes = (next: Set<string>) => setField('mcpTools', [...next].sort());
  const toggleMany = (scopes: string[], on: boolean) => {
    const next = new Set(granted);
    for (const s of scopes) {
      if (on) next.add(s);
      else next.delete(s);
    }
    setScopes(next);
  };

  // ── Platform tool scopes ────────────────────────────────────────────────────
  const platformScopes = catalogues.toolScopes;
  const platformGranted = useMemo(() => new Set(fields.toolScopes), [fields.toolScopes]);
  const togglePlatform = (scopes: string[], on: boolean) => {
    const next = new Set(platformGranted);
    for (const s of scopes) {
      if (on) next.add(s);
      else next.delete(s);
    }
    setField('toolScopes', [...next].sort());
  };
  const platformAll = platformScopes.length > 0 && platformGranted.size === platformScopes.length;
  const platformSome = platformGranted.size > 0 && !platformAll;
  const platformOpen = !!expanded.__platform;

  // ── Effective access preview ─────────────────────────────────────────────
  // Resolved live as the user edits, so the scopes being assigned are visible
  // on the page that assigns them (same panel the connectivity tab uses).
  const [access, setAccess] = useState<EffectiveAccess | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
      void previewEffectiveAccess({
        connectivity: {
          mode: fields.connectivityMode,
          hosts: fields.connectivityHosts
            .split('\n')
            .map((h) => h.trim())
            .filter(Boolean),
        },
        connectors: fields.connectors,
        environment_id: fields.environmentId || null,
      }).then((result) => {
        setAccess(result.access);
        setAccessError(result.error);
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [fields.connectivityMode, fields.connectivityHosts, fields.connectors, fields.environmentId]);

  // ── Connections ─────────────────────────────────────────────────────────────
  const connectorFor = (name: string): Connector | undefined =>
    fields.connectors.find((c) => c.connection === name);
  const setConnectorActions = (name: string, actions: string[]) => {
    const others = fields.connectors.filter((c) => c.connection !== name);
    setField(
      'connectors',
      actions.length > 0 ? [...others, { connection: name, actions }] : others,
    );
  };

  return (
    <Stack gap="relaxed">
      <StepHeader
        icon="wrench"
        title="Tools & access"
        description="Grant access to MCP servers. Every tool call is checked against these scopes before it runs."
      />

      <Text as="span" size="supporting" color="muted">
        <Text as="span" size="supporting" weight="heavy" color="accent">
          {grantedServers.length}
        </Text>{' '}
        servers ·{' '}
        <Text as="span" size="supporting" weight="heavy">
          {fields.mcpTools.length}
        </Text>{' '}
        tools granted
      </Text>

      {catalogues.status.mcp === 'error' && (
        <Alert variant="error">MCP servers failed to load: {catalogues.errors.mcp}</Alert>
      )}

      {/* ── Server tree ────────────────────────────────────────────────────── */}
      <Stack gap="snug">
        {tree.map((server) => {
          const grantedHere = server.scopes.filter((sc) => granted.has(sc));
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
                    <Separator span="full" />
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
                      const domainGranted = domainScopes.filter((sc) => granted.has(sc));
                      const domainAll = domainGranted.length === domainScopes.length;
                      const domainSome = domainGranted.length > 0 && !domainAll;
                      return (
                        <Stack key={domain.domain}>
                          <Separator span="full" />
                          <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                            <Checkbox
                              size="sm"
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
                            const toolOn = granted.has(tool.scope);
                            return (
                              <Stack key={tool.scope}>
                                <Separator span="full" />
                                <Stack
                                  direction="horizontal"
                                  align="start"
                                  gap="cozy"
                                  padding="cozy"
                                  paddingInline="relaxed"
                                >
                                  <Checkbox
                                    size="sm"
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

      <Separator span="full" />

      {/* ── Platform tools ─────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel
          title="Platform tools"
          desc="Direct platform capabilities granted to the agent, beyond MCP servers."
        />
        {catalogues.status.toolScopes === 'error' && (
          <Alert variant="error">Tool scopes failed to load: {catalogues.errors.toolScopes}</Alert>
        )}
        <Card selected={platformGranted.size > 0} selectedVariant="outline">
          <Collapsible
            open={platformOpen}
            onOpenChange={(next) => setExpanded((e) => ({ ...e, __platform: next }))}
          >
            <CardContent flush>
              <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                <Checkbox
                  size="md"
                  checked={platformAll}
                  indeterminate={platformSome}
                  onChange={(checked) =>
                    togglePlatform(
                      platformScopes.map((s) => s.scope),
                      checked,
                    )
                  }
                  aria-label="Grant all platform tools"
                />
                <Icon
                  name="zap"
                  size="sm"
                  color={platformGranted.size > 0 ? 'accent' : 'muted'}
                  container
                  containerVariant={platformGranted.size > 0 ? 'accent' : 'surface-subtle'}
                  label=""
                />
                <Stack grow gap="tight">
                  <Text as="span" size="supporting" weight="heavy">
                    Orbit platform
                  </Text>
                  <Text as="span" size="micro" mono color="subtle">
                    {platformScopes.length} capabilities
                  </Text>
                </Stack>
                <Text
                  as="span"
                  size="micro"
                  weight="heavy"
                  color={platformAll ? 'accent' : 'muted'}
                >
                  {platformGranted.size > 0
                    ? platformAll
                      ? `All ${platformScopes.length} granted`
                      : `${platformGranted.size} of ${platformScopes.length} granted`
                    : ''}
                </Text>
                <CollapsibleTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`${platformOpen ? 'Collapse' : 'Expand'} platform tools`}
                  >
                    <Icon name="chevron-down" rotate={platformOpen ? 180 : 0} />
                  </Button>
                </CollapsibleTrigger>
              </Stack>
              <CollapsibleContent>
                {platformScopes.map((s) => {
                  const on = platformGranted.has(s.scope);
                  return (
                    <Stack key={s.scope}>
                      <Separator span="full" />
                      <Stack
                        direction="horizontal"
                        align="start"
                        gap="cozy"
                        padding="cozy"
                        paddingInline="relaxed"
                      >
                        <Checkbox
                          size="sm"
                          checked={on}
                          onChange={(checked) => togglePlatform([s.scope], checked)}
                          aria-label={`Grant ${s.scope}`}
                        />
                        <Stack
                          gap="tight"
                          minWidth="form-field"
                          maxWidth="form-field"
                          shrink={false}
                        >
                          <Text as="span" size="caption" mono weight="heavy" truncate>
                            {s.scope}
                          </Text>
                        </Stack>
                        <Text size="caption" color="muted">
                          {s.description}
                        </Text>
                      </Stack>
                    </Stack>
                  );
                })}
              </CollapsibleContent>
            </CardContent>
          </Collapsible>
        </Card>
      </Stack>

      <Separator span="full" />

      {/* ── Connections ────────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel
          title="Connections"
          desc="Credentialed connections the agent may call. Credentials are injected just-in-time. The agent never sees them."
        />
        {catalogues.status.connections === 'error' && (
          <Alert variant="error">Connections failed to load: {catalogues.errors.connections}</Alert>
        )}
        {catalogues.connections.length === 0 && catalogues.status.connections === 'success' && (
          <Text size="caption" color="subtle">
            No connections are registered in this tenant yet.
          </Text>
        )}
        <Stack gap="snug">
          {catalogues.connections.map((conn) => {
            const current = connectorFor(conn.name)?.actions ?? [];
            const on = current.length > 0;
            const whole = on && current.length === conn.actions.length;
            const key = `__conn_${conn.name}`;
            const isOpen = !!expanded[key];
            return (
              <Card key={conn.name} selected={on} selectedVariant="outline">
                <Collapsible
                  open={isOpen}
                  onOpenChange={(next) => setExpanded((e) => ({ ...e, [key]: next }))}
                >
                  <CardContent flush>
                    <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                      <Checkbox
                        size="md"
                        checked={whole}
                        indeterminate={on && !whole}
                        onChange={(checked) =>
                          setConnectorActions(conn.name, checked ? [...conn.actions] : [])
                        }
                        aria-label={`Grant all ${conn.name} actions`}
                      />
                      <Icon
                        name="key"
                        size="sm"
                        color={on ? 'accent' : 'muted'}
                        container
                        containerVariant={on ? 'accent' : 'surface-subtle'}
                        label=""
                      />
                      <Stack grow gap="tight">
                        <Stack direction="horizontal" align="center" gap="snug">
                          <Text as="span" size="supporting" weight="heavy">
                            {conn.name}
                          </Text>
                          <Badge variant="outline" size="sm">
                            {conn.adapter}
                          </Badge>
                        </Stack>
                        <Text as="span" size="micro" mono color="subtle">
                          connection:{conn.name}
                        </Text>
                      </Stack>
                      <Text
                        as="span"
                        size="micro"
                        weight="heavy"
                        color={whole ? 'accent' : 'muted'}
                      >
                        {on
                          ? whole
                            ? `All ${conn.actions.length} actions`
                            : `${current.length} of ${conn.actions.length} actions`
                          : ''}
                      </Text>
                      <CollapsibleTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`${isOpen ? 'Collapse' : 'Expand'} ${conn.name} actions`}
                        >
                          <Icon name="chevron-down" rotate={isOpen ? 180 : 0} />
                        </Button>
                      </CollapsibleTrigger>
                    </Stack>
                    <CollapsibleContent>
                      {conn.actions.map((action) => {
                        const actionOn = current.includes(action);
                        return (
                          <Stack key={action}>
                            <Separator span="full" />
                            <Stack
                              direction="horizontal"
                              align="center"
                              gap="cozy"
                              padding="cozy"
                              paddingInline="relaxed"
                            >
                              <Checkbox
                                size="sm"
                                checked={actionOn}
                                onChange={(checked) =>
                                  setConnectorActions(
                                    conn.name,
                                    checked
                                      ? [...current, action]
                                      : current.filter((a) => a !== action),
                                  )
                                }
                                aria-label={`Grant ${conn.name} ${action}`}
                              />
                              <Text as="span" size="caption" mono weight="heavy">
                                {action}
                              </Text>
                              <Stack grow />
                              <Text as="span" size="micro" mono color="subtle">
                                connection:{conn.name}:{action}
                              </Text>
                            </Stack>
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
      </Stack>

      <Separator span="full" />

      {/* ── Network egress ─────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel title="Network egress" desc="The hosts the agent may reach directly." />
        <RadioGroup
          value={fields.connectivityMode}
          onValueChange={(v) => setField('connectivityMode', v as ConnectivityMode)}
          aria-label="Network egress mode"
        >
          <Grid cols={3} gap="cozy">
            {EGRESS_MODES.map((m) => {
              const selected = fields.connectivityMode === m.id;
              return (
                <Card
                  key={m.id}
                  interactive
                  selected={selected}
                  onClick={() => setField('connectivityMode', m.id)}
                >
                  <CardContent padded compact>
                    <Stack gap="snug">
                      <Stack direction="horizontal" align="center">
                        <Icon
                          name={m.icon}
                          size="sm"
                          color={selected ? 'accent' : 'muted'}
                          container
                          containerVariant={selected ? 'accent' : 'surface-subtle'}
                          label=""
                        />
                        <Stack grow />
                        <RadioGroupItem value={m.id} aria-label={m.name} />
                      </Stack>
                      <Stack gap="tight">
                        <Text as="span" size="supporting" weight="heavy">
                          {m.name}
                        </Text>
                        <Text size="micro" color="muted">
                          {m.desc}
                        </Text>
                      </Stack>
                    </Stack>
                  </CardContent>
                </Card>
              );
            })}
          </Grid>
        </RadioGroup>
        {fields.connectivityMode === 'whitelist' && (
          <Stack gap="tight">
            <Textarea
              value={fields.connectivityHosts}
              onChange={(e) => setField('connectivityHosts', e.target.value)}
              rows={3}
              placeholder={'api.github.com\n*.internal.example.com'}
              aria-label="Allowed hosts"
            />
            <Text size="micro" color="subtle">
              One host per line. Exact,{' '}
              <Badge variant="outline" mono size="sm">
                *
              </Badge>
              , or{' '}
              <Badge variant="outline" mono size="sm">
                *.suffix
              </Badge>{' '}
              patterns.
            </Text>
          </Stack>
        )}
      </Stack>

      <Separator span="full" />

      {/* ── Environment ────────────────────────────────────────────────────── */}
      <Stack gap="cozy">
        <SectionLabel
          title="Environment"
          desc="An environment merges its egress hosts and connections into the agent's effective access. Choose it alongside the access it modifies."
        />
        <Card selected={!!fields.environmentId} selectedVariant="outline">
          <CardContent padded compact>
            <Stack direction="horizontal" align="center" gap="cozy">
              <Icon
                name="layers"
                size="sm"
                color={fields.environmentId ? 'accent' : 'muted'}
                container
                containerVariant={fields.environmentId ? 'accent' : 'surface-subtle'}
                label=""
              />
              <Stack grow gap="tight">
                <Text as="span" size="supporting" weight="heavy">
                  Environment
                </Text>
                {(() => {
                  const env = catalogues.environments.find((e) => e.id === fields.environmentId);
                  return (
                    <Text as="span" size="micro" color="subtle">
                      {env
                        ? `Merges ${env.egress_hosts.length} hosts and ${env.connectors.length} connections into effective access.`
                        : 'No environment. The agent has exactly the access configured above.'}
                    </Text>
                  );
                })()}
              </Stack>
              <Stack minWidth="form-field" maxWidth="form-field">
                <NativeSelect
                  value={fields.environmentId}
                  onChange={(e) => setField('environmentId', e.target.value)}
                  aria-label="Environment"
                >
                  <option value="">None</option>
                  {catalogues.environments.map((env) => (
                    <option key={env.id} value={env.id}>
                      {env.name}
                    </option>
                  ))}
                </NativeSelect>
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </Stack>

      <EffectiveAccessPanel access={access} loading={!access && !accessError} error={accessError} />
    </Stack>
  );
}
