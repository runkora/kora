'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { useMemo, useState } from 'react';
import {
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
import type { ToolScopeDefinition } from '@orbit/shared/api';

export function PlatformToolsCard({
  scopes,
  granted,
  onChange,
}: {
  scopes: ToolScopeDefinition[];
  granted: string[];
  onChange: (scopes: string[]) => void;
}) {
  const [open, setOpen] = useState(false);

  const grantedSet = useMemo(() => new Set(granted), [granted]);
  const toggle = (toToggle: string[], on: boolean) => {
    const next = new Set(grantedSet);
    for (const s of toToggle) {
      if (on) next.add(s);
      else next.delete(s);
    }
    onChange([...next].sort());
  };
  const all = scopes.length > 0 && grantedSet.size === scopes.length;
  const some = grantedSet.size > 0 && !all;

  return (
    <Card selected={grantedSet.size > 0} selectedVariant="outline">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardContent flush>
          <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
            <Checkbox
              size="md"
              checked={all}
              indeterminate={some}
              onChange={(checked) =>
                toggle(
                  scopes.map((s) => s.scope),
                  checked,
                )
              }
              aria-label="Grant all platform tools"
            />
            <Icon
              name="zap"
              size="sm"
              color={grantedSet.size > 0 ? 'accent' : 'muted'}
              container
              containerVariant={grantedSet.size > 0 ? 'accent' : 'surface-subtle'}
              label=""
            />
            <Stack grow gap="tight">
              <Text as="span" size="supporting" weight="heavy">
                Orbit platform
              </Text>
              <Text as="span" size="micro" mono color="subtle">
                {scopes.length} capabilities
              </Text>
            </Stack>
            <Text as="span" size="micro" weight="heavy" color={all ? 'accent' : 'muted'}>
              {grantedSet.size > 0
                ? all
                  ? `All ${scopes.length} granted`
                  : `${grantedSet.size} of ${scopes.length} granted`
                : ''}
            </Text>
            <CollapsibleTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`${open ? 'Collapse' : 'Expand'} platform tools`}
              >
                <Icon name="chevron-down" rotate={open ? 180 : 0} />
              </Button>
            </CollapsibleTrigger>
          </Stack>
          <CollapsibleContent>
            {scopes.map((s) => {
              const on = grantedSet.has(s.scope);
              return (
                <Stack key={s.scope} gap="none">
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
                      onChange={(checked) => toggle([s.scope], checked)}
                      aria-label={`Grant ${s.scope}`}
                    />
                    <Stack gap="tight" minWidth="form-field" maxWidth="form-field" shrink={false}>
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
  );
}
