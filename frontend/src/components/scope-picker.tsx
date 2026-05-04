/**
 * @tier 2 — App-level shared. Cross-feature; used by policy (grants, roles tabs)
 * and model-gateway (create-token, edit-scopes modals). Renders a searchable
 * accordion of Orbit scope strings grouped by resource type, with per-group and
 * per-scope checkbox selection.
 * @promote-to-tier-1 if: the Orbit scope format and resource-type grouping are
 * removed and a generic multi-select picker is needed outside the dashboard.
 */
'use client';

import { useMemo, useState } from 'react';
import { Button, Card, Checkbox, Icon, Input, Separator, Stack, Text } from '@orbit/terra';
// Direct relative import — importing via @orbit/components barrel would be
// circular (scope-picker is itself exported from that barrel).
import { ScopeChip } from './scope-chip';

export interface ScopePickerProps {
  availableScopes: string[];
  selectedScopes: string[];
  onToggle: (scope: string, checked: boolean) => void;
  onToggleGroup: (scopes: string[], checked: boolean) => void;
  search: string;
  onSearchChange: (value: string) => void;
}

interface ScopeGroup {
  resourceType: string;
  scopes: string[];
}

function groupScopes(scopes: string[]): ScopeGroup[] {
  const map = new Map<string, string[]>();
  for (const scope of scopes) {
    const rt = scope.split(':')[0];
    if (!map.has(rt)) map.set(rt, []);
    map.get(rt)!.push(scope);
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([resourceType, groupScopes]) => ({ resourceType, scopes: groupScopes }));
}

export function ScopePicker({
  availableScopes,
  selectedScopes,
  onToggle,
  onToggleGroup,
  search,
  onSearchChange,
}: ScopePickerProps) {
  const filtered = useMemo(() => {
    if (!search) return availableScopes;
    const q = search.toLowerCase();
    return availableScopes.filter((s) => s.toLowerCase().includes(q));
  }, [availableScopes, search]);

  const groups = useMemo(() => groupScopes(filtered), [filtered]);

  const [manualOpen, setManualOpen] = useState<Record<string, boolean>>({});

  const isGroupOpen = (rt: string, group: ScopeGroup) => {
    if (search) return true;
    if (manualOpen[rt] !== undefined) return manualOpen[rt];
    return group.scopes.some((s) => selectedScopes.includes(s));
  };

  const toggleGroupOpen = (rt: string) => {
    setManualOpen((prev) => ({ ...prev, [rt]: !prev[rt] }));
  };

  return (
    <Stack gap="tight">
      {/* Search */}
      <Input
        type="text"
        placeholder="Filter scopes..."
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
      />

      {/* Accordion */}
      <Card maxHeight="scroll-md" overflowY="auto">
        {groups.length === 0 ? (
          <Stack paddingBlock="comfortable" paddingInline="snug" align="center">
            <Text size="caption" color="muted">
              No scopes match your filter
            </Text>
          </Stack>
        ) : (
          groups.map((group) => {
            const open = isGroupOpen(group.resourceType, group);
            const selectedInGroup = group.scopes.filter((s) => selectedScopes.includes(s));
            const allSelected = selectedInGroup.length === group.scopes.length;
            const someSelected = selectedInGroup.length > 0 && !allSelected;

            return (
              <Stack key={group.resourceType}>
                {/* Group header */}
                <Button
                  type="button"
                  variant="ghost"
                  fullWidth
                  onClick={() => toggleGroupOpen(group.resourceType)}
                >
                  <Stack direction="horizontal" align="center" gap="snug" grow>
                    <Icon name="chevron-right" size="2xs" color="subtle" rotate={open ? 90 : 0} />
                    <Text as="span" size="micro" color="accent" mono weight="emphasis">
                      {group.resourceType}
                    </Text>
                    {selectedInGroup.length > 0 && (
                      <Text size="micro" color="accent" weight="emphasis">
                        {selectedInGroup.length} selected
                      </Text>
                    )}
                    <Text size="micro" color="subtle">
                      {group.scopes.length} {group.scopes.length === 1 ? 'scope' : 'scopes'}
                    </Text>
                  </Stack>
                </Button>
                <Separator />

                {/* Group body */}
                {open && (
                  <Stack>
                    {/* Select all */}
                    {group.scopes.length > 1 && (
                      <Checkbox
                        variant="row"
                        checked={allSelected}
                        indeterminate={someSelected}
                        onChange={(checked) => onToggleGroup(group.scopes, checked)}
                        paddingBlock="tight"
                        paddingInline="snug"
                        gap="snug"
                      >
                        <Text size="caption" color="subtle">
                          Select all {group.resourceType} scopes
                        </Text>
                      </Checkbox>
                    )}

                    {/* Individual scopes */}
                    {group.scopes.map((scope) => (
                      <Checkbox
                        key={scope}
                        variant="row"
                        checked={selectedScopes.includes(scope)}
                        onChange={(checked) => onToggle(scope, checked)}
                        paddingBlock="tight"
                        paddingInline="snug"
                        gap="snug"
                      >
                        <ScopeChip scope={scope} />
                      </Checkbox>
                    ))}
                  </Stack>
                )}
              </Stack>
            );
          })
        )}
      </Card>
    </Stack>
  );
}
