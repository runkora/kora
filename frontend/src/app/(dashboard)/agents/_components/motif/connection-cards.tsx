'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

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
  Icon,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector } from '@orbit/shared/api';

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

  const connectorFor = (name: string): Connector | undefined =>
    value.find((c) => c.connection === name);
  const setConnectorActions = (name: string, actions: string[]) => {
    const others = value.filter((c) => c.connection !== name);
    onChange(actions.length > 0 ? [...others, { connection: name, actions }] : others);
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
        const current = connectorFor(conn.name)?.actions ?? [];
        const on = current.length > 0;
        const whole = on && current.length === conn.actions.length;
        const isOpen = !!expanded[conn.name];
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
  );
}
