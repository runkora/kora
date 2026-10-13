'use client';

/** @tier 2 — Orbit-domain-aware: scope selection grouped by resource. Used by: policy grants, policy roles, model-gateway token modals. */

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
  Input,
  Separator,
  Stack,
  Text,
} from '@orbit/hades';

export interface ScopePickerProps {
  /** All selectable scope strings (e.g. `agents:runs:read`). */
  scopes: string[];
  /** Currently selected scopes. */
  selected: string[];
  /** Called with the full next selection whenever any checkbox changes. */
  onChange: (scopes: string[]) => void;
  /** Adds a search input that filters the scopes; groups with matches open while it is in use. */
  searchable?: boolean;
}

/**
 * ScopePicker
 *
 * Tri-state scope selector. Groups scope strings by their first `:`-segment
 * (resource type) and renders one collapsible outline-selected card per group:
 * a group-level checkbox plus one row per scope when expanded.
 */
export function ScopePicker({ scopes, selected, onChange, searchable = false }: ScopePickerProps) {
  const [search, setSearch] = useState('');
  const query = searchable ? search.trim().toLowerCase() : '';

  const groups = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const scope of scopes) {
      if (query && !scope.toLowerCase().includes(query)) continue;
      const group = scope.split(':')[0];
      if (!map.has(group)) map.set(group, []);
      map.get(group)!.push(scope);
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [scopes, query]);

  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const toggle = (toToggle: string[], on: boolean) => {
    const next = new Set(selectedSet);
    for (const s of toToggle) {
      if (on) next.add(s);
      else next.delete(s);
    }
    onChange([...next]);
  };

  return (
    <Stack gap="cozy">
      {searchable && (
        <Input
          type="text"
          placeholder="Filter scopes..."
          aria-label="Filter scopes"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      )}
      {groups.length === 0 && query ? (
        <Stack paddingBlock="comfortable" align="center">
          <Text size="caption" color="muted">
            No scopes match your filter
          </Text>
        </Stack>
      ) : (
        groups.map(([group, groupScopes]) => (
          <ScopeGroupCard
            key={group}
            group={group}
            scopes={groupScopes}
            selectedSet={selectedSet}
            forceOpen={query !== ''}
            onToggle={toggle}
          />
        ))
      )}
    </Stack>
  );
}

function ScopeGroupCard({
  group,
  scopes,
  selectedSet,
  forceOpen,
  onToggle,
}: {
  group: string;
  scopes: string[];
  selectedSet: Set<string>;
  forceOpen: boolean;
  onToggle: (scopes: string[], on: boolean) => void;
}) {
  const selectedInGroup = scopes.filter((s) => selectedSet.has(s)).length;
  const any = selectedInGroup > 0;

  // Until the user opens or closes the group, it follows the selection: a group that holds a
  // scope is open, so existing grants are visible even when the saved scopes load after mount.
  const [manualOpen, setManualOpen] = useState<boolean | null>(null);
  const open = forceOpen || (manualOpen ?? any);
  const all = scopes.length > 0 && selectedInGroup === scopes.length;
  const some = selectedInGroup > 0 && !all;

  return (
    <Card selected={any} selectedVariant="outline">
      <Collapsible open={open} onOpenChange={setManualOpen}>
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
