'use client';

/** @tier 2 — Orbit-domain-aware: per-connection action grants as collapsible tri-state cards, with grant-time resource scoping for qualifier-capable adapters. Used by: agents builder, agents configuration, environments. */

import { useState } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Checkbox,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Eyebrow,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type {
  ConnectionCatalogEntry,
  ConnectionQualifierEntry,
  Connector,
} from '@orbit/shared/api';

/** Pluralizes a qualifier dimension name — deliberately dumb ("y" → "ies", else
 * append "s"), which covers the dimensions in play: repository, namespace. */
const plural = (name: string) => (name.endsWith('y') ? `${name.slice(0, -1)}ies` : `${name}s`);

const countLabel = (n: number, name: string) => `${n} ${n === 1 ? name : plural(name)}`;

/**
 * Compiled scope preview for one action row — mirrors Go agents.ConnectorScopes.
 * Unrestricted (no resources) or single-word actions stay unqualified; with
 * resources the action fragment splits at its last colon into category/verb and
 * the first resource's qualified form is shown, with a count for the rest.
 */
function actionScopePreview(connection: string, action: string, resources: string[]): string {
  const i = action.lastIndexOf(':');
  if (resources.length === 0 || i <= 0) {
    return `connection:${connection}:${action}`;
  }
  const category = action.slice(0, i);
  const verb = action.slice(i + 1);
  const first = `connection:${connection}:${category}:${resources[0]}:${verb}`;
  return resources.length === 1 ? first : `${first} +${resources.length - 1} more`;
}

/**
 * The resource scope block for a qualifier-capable connection: an all/selected
 * radio pair labeled from the adapter's qualifier dimension, and under
 * "Selected" a multiselect composed from existing Terra primitives, a Badge
 * chip per resource plus an Input for manual entry. The attachment is what
 * narrows an agent, so anything the upstream credential can reach is valid here.
 */
function ResourceScopeBlock({
  connection,
  qualifier,
  resources,
  onResourcesChange,
}: {
  connection: string;
  qualifier: ConnectionQualifierEntry;
  resources: string[];
  onResourcesChange: (resources: string[]) => void;
}) {
  // "Selected" chosen explicitly while the chip list is still empty. An empty
  // selection compiles identically to "All", so this is pure UI state.
  const [selectedMode, setSelectedMode] = useState(false);
  const [draft, setDraft] = useState('');

  const mode = selectedMode || resources.length > 0 ? 'selected' : 'all';
  const addResource = (raw: string) => {
    const v = raw.trim().toLowerCase();
    if (v && !resources.includes(v)) {
      onResourcesChange([...resources, v]);
    }
  };

  return (
    <Stack gap="cozy" paddingBlock="cozy" paddingInline="relaxed">
      <Stack gap="tight">
        <Eyebrow>Resource scope</Eyebrow>
        <Text as="span" size="micro" color="subtle">
          Which {plural(qualifier.name)} this connector may touch.
        </Text>
      </Stack>
      <RadioGroup
        value={mode}
        onValueChange={(v) => {
          if (v === 'all') {
            setSelectedMode(false);
            onResourcesChange([]);
          } else {
            setSelectedMode(true);
          }
        }}
        aria-label={`${connection} resource scope`}
      >
        <Grid cols={2} gap="cozy">
          <RadioCard
            value="all"
            selected={mode === 'all'}
            onSelect={() => {
              setSelectedMode(false);
              onResourcesChange([]);
            }}
            icon="globe"
            title={`All ${plural(qualifier.name)}`}
            description={`Everything the connection reaches, including ${plural(qualifier.name)} added later`}
          />
          <RadioCard
            value="selected"
            selected={mode === 'selected'}
            onSelect={() => setSelectedMode(true)}
            icon="filter"
            title={`Selected ${plural(qualifier.name)}`}
            description={`Only the ${plural(qualifier.name)} you pick below`}
          />
        </Grid>
      </RadioGroup>
      {mode === 'selected' && (
        <Stack gap="snug">
          {resources.length > 0 && (
            <Stack direction="horizontal" align="center" gap="tight" wrap>
              {resources.map((r) => (
                <Button
                  key={r}
                  variant="outline"
                  size="xs"
                  onClick={() => onResourcesChange(resources.filter((x) => x !== r))}
                  aria-label={`Remove ${qualifier.name} ${r}`}
                >
                  <Text as="span" size="micro" mono>
                    {r}
                  </Text>
                  <Icon name="close" size="xs" color="muted" />
                </Button>
              ))}
            </Stack>
          )}
          <Stack direction="horizontal" align="center" gap="snug">
            <Stack maxWidth="card" grow>
              <Input
                compact
                mono
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    addResource(draft);
                    setDraft('');
                  }
                }}
                placeholder={`Add a ${qualifier.name}…`}
                aria-label={`Add a ${qualifier.name} to ${connection}`}
              />
            </Stack>
            <Text as="span" size="micro" color="subtle">
              Enter to add
            </Text>
          </Stack>
        </Stack>
      )}
    </Stack>
  );
}

export function ConnectionCards({
  available,
  value,
  onChange,
}: {
  available: ConnectionCatalogEntry[];
  value: Connector[];
  onChange: (connectors: Connector[]) => void;
}) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  // Resource picks made before any action is selected. A connector with zero
  // actions is not emitted (it grants nothing), so its resources have nowhere
  // to persist yet — hold them here so the picker works before the actions are
  // chosen, and fold them into the connector the moment an action turns on.
  const [pendingResources, setPendingResources] = useState<Record<string, string[]>>({});

  const connectorFor = (name: string): Connector | undefined =>
    value.find((c) => c.connection === name);
  const emit = (name: string, actions: string[], resources: string[] | undefined) => {
    const others = value.filter((c) => c.connection !== name);
    if (actions.length === 0) {
      onChange(others);
      return;
    }
    const next: Connector = { connection: name, actions };
    if (resources && resources.length > 0) {
      next.resources = resources;
    }
    onChange([...others, next]);
  };
  const setConnectorActions = (name: string, actions: string[]) =>
    emit(name, actions, connectorFor(name)?.resources ?? pendingResources[name]);
  const setConnectorResources = (name: string, resources: string[]) => {
    setPendingResources((prev) => ({ ...prev, [name]: resources }));
    const current = connectorFor(name);
    if (current) {
      emit(name, current.actions, resources);
    }
  };

  if (available.length === 0) {
    return (
      <Text size="caption" color="subtle">
        No connections are registered in this tenant yet.
      </Text>
    );
  }

  return (
    <Stack gap="snug">
      {available.map((conn) => {
        const connector = connectorFor(conn.name);
        const current = connector?.actions ?? [];
        const resources = connector?.resources ?? pendingResources[conn.name] ?? [];
        const on = current.length > 0;
        const whole = on && current.length === conn.actions.length;
        const isOpen = !!expanded[conn.name];
        const qualifier = conn.qualifier ?? null;
        const actionsSummary = whole
          ? `All ${conn.actions.length} actions`
          : `${current.length} of ${conn.actions.length} actions`;
        const summary = on
          ? resources.length > 0 && qualifier
            ? `${actionsSummary} · ${countLabel(resources.length, qualifier.name)}`
            : actionsSummary
          : '';
        return (
          <Card key={conn.name} selected={on} selectedVariant="outline">
            <Collapsible
              open={isOpen}
              onOpenChange={(next) => setExpanded((e) => ({ ...e, [conn.name]: next }))}
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
                  <Text as="span" size="micro" weight="heavy" color={whole ? 'accent' : 'muted'}>
                    {summary}
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
                  {qualifier && (
                    <Stack gap="none">
                      <Separator span="full" />
                      <ResourceScopeBlock
                        connection={conn.name}
                        qualifier={qualifier}
                        resources={resources}
                        onResourcesChange={(next) => setConnectorResources(conn.name, next)}
                      />
                    </Stack>
                  )}
                  {conn.actions.map((action) => {
                    const actionOn = current.includes(action);
                    return (
                      <Stack key={action} gap="none">
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
                            {actionScopePreview(conn.name, action, resources)}
                          </Text>
                        </Stack>
                      </Stack>
                    );
                  })}
                  {on &&
                    conn.baseline.map((fragment) => (
                      <Stack key={`baseline-${fragment}`} gap="none">
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
                            checked
                            disabled
                            aria-label={`${conn.name} ${fragment} is added automatically`}
                          />
                          <Stack gap="tight">
                            <Text as="span" size="caption" mono weight="heavy">
                              {fragment}
                            </Text>
                            <Text as="span" size="micro" color="subtle">
                              Added automatically
                            </Text>
                          </Stack>
                          <Stack grow />
                          <Text as="span" size="micro" mono color="subtle">
                            connection:{conn.name}:{fragment}
                          </Text>
                        </Stack>
                      </Stack>
                    ))}
                </CollapsibleContent>
              </CardContent>
            </Collapsible>
          </Card>
        );
      })}
    </Stack>
  );
}
