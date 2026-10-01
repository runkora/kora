'use client';

/** @tier 2 — Orbit-domain-aware: tri-state scope selection grouped by resource. Used by: policy grants, policy roles. */

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

export interface ScopeTreePickerProps {
  /** All selectable scope strings (e.g. `agents:runs:read`). */
  scopes: string[];
  /** Currently selected scopes. */
  selected: string[];
  /** Called with the full next selection whenever any checkbox changes. */
  onChange: (scopes: string[]) => void;
}

/**
 * ScopeTreePicker
 *
 * Tri-state scope selector. Groups bare scope strings by their first
 * `:`-segment (resource type) and renders one collapsible outline-selected
 * card per group: a group-level checkbox (checked / indeterminate) plus one
 * row per scope when expanded.
 */
export function ScopeTreePicker({ scopes, selected, onChange }: ScopeTreePickerProps) {
  const groups = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const scope of scopes) {
      const group = scope.split(':')[0];
      if (!map.has(group)) map.set(group, []);
      map.get(group)!.push(scope);
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [scopes]);

  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const toggle = (toToggle: string[], on: boolean) => {
    const next = new Set(selectedSet);
    for (const s of toToggle) {
      if (on) next.add(s);
      else next.delete(s);
    }
    onChange([...next].sort());
  };

  return (
    <Stack gap="cozy">
      {groups.map(([group, groupScopes]) => (
        <ScopeGroupCard
          key={group}
          group={group}
          scopes={groupScopes}
          selectedSet={selectedSet}
          onToggle={toggle}
        />
      ))}
    </Stack>
  );
}

function ScopeGroupCard({
  group,
  scopes,
  selectedSet,
  onToggle,
}: {
  group: string;
  scopes: string[];
  selectedSet: Set<string>;
  onToggle: (scopes: string[], on: boolean) => void;
}) {
  const [open, setOpen] = useState(false);

  const selectedInGroup = scopes.filter((s) => selectedSet.has(s)).length;
  const all = scopes.length > 0 && selectedInGroup === scopes.length;
  const some = selectedInGroup > 0 && !all;
  const any = selectedInGroup > 0;

  return (
    <Card selected={any} selectedVariant="outline">
      <Collapsible open={open} onOpenChange={setOpen}>
        <CardContent flush>
          <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
            <Checkbox
              size="md"
              checked={all}
              indeterminate={some}
              onChange={(checked) => onToggle(scopes, checked)}
              aria-label={`Select all ${group} scopes`}
            />
            <Icon
              name="shield"
              size="sm"
              color={any ? 'accent' : 'muted'}
              container
              containerVariant={any ? 'accent' : 'surface-subtle'}
              label=""
            />
            <Stack grow gap="tight">
              <Text as="span" size="supporting" weight="heavy">
                {group}
              </Text>
              <Text as="span" size="micro" mono color="subtle">
                {selectedInGroup}/{scopes.length} scopes
              </Text>
            </Stack>
            <Text as="span" size="micro" weight="heavy" color={all ? 'accent' : 'muted'}>
              {any
                ? all
                  ? `All ${scopes.length} granted`
                  : `${selectedInGroup} of ${scopes.length} granted`
                : ''}
            </Text>
            <CollapsibleTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`${open ? 'Collapse' : 'Expand'} ${group} scopes`}
              >
                <Icon name="chevron-down" rotate={open ? 180 : 0} />
              </Button>
            </CollapsibleTrigger>
          </Stack>
          <CollapsibleContent>
            {scopes.map((scope) => (
              <Stack key={scope} gap="none">
                <Separator />
                <Stack
                  direction="horizontal"
                  align="center"
                  gap="cozy"
                  padding="cozy"
                  paddingInline="relaxed"
                >
                  <Checkbox
                    size="sm"
                    checked={selectedSet.has(scope)}
                    onChange={(checked) => onToggle([scope], checked)}
                    aria-label={`Select ${scope}`}
                  />
                  <Stack grow minWidth="none">
                    <Text as="span" size="caption" mono weight="heavy" truncate>
                      {scope}
                    </Text>
                  </Stack>
                </Stack>
              </Stack>
            ))}
          </CollapsibleContent>
        </CardContent>
      </Collapsible>
    </Card>
  );
}
