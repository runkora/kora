'use client';

import { useMemo, useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { Text } from '@orbit/terra';
import { ScopeChip } from './scope-chip';

interface ScopePickerProps {
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

  // Auto-expand groups that have selected scopes, or all when searching
  const [manualOpen, setManualOpen] = useState<Record<string, boolean>>({});

  const isGroupOpen = (rt: string, group: ScopeGroup) => {
    if (search) return true; // always expand when filtering
    if (manualOpen[rt] !== undefined) return manualOpen[rt];
    // Default: expand if any scope in this group is selected
    return group.scopes.some((s) => selectedScopes.includes(s));
  };

  const toggleGroupOpen = (rt: string) => {
    setManualOpen((prev) => ({ ...prev, [rt]: !prev[rt] }));
  };

  return (
    <section>
      {/* Search */}
      <span className="relative mb-1.5 block">
        <input
          type="text"
          placeholder="Filter scopes..."
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
      </span>

      {/* Accordion */}
      <section className="border border-border rounded-lg overflow-hidden max-h-60 overflow-y-auto">
        {groups.length === 0 ? (
          <section className="px-3 py-4 text-center">
            <Text size="caption" color="muted">
              No scopes match your filter
            </Text>
          </section>
        ) : (
          groups.map((group) => {
            const open = isGroupOpen(group.resourceType, group);
            const selectedInGroup = group.scopes.filter((s) => selectedScopes.includes(s));
            const allSelected = selectedInGroup.length === group.scopes.length;
            const someSelected = selectedInGroup.length > 0 && !allSelected;

            return (
              <section
                key={group.resourceType}
                className="border-b border-border-subtle last:border-b-0"
              >
                {/* Group header */}
                <button
                  type="button"
                  onClick={() => toggleGroupOpen(group.resourceType)}
                  className="flex w-full items-center gap-2.5 px-3 py-2 bg-surface-overlay hover:bg-surface-elevated cursor-pointer select-none transition-colors"
                >
                  <ChevronRight
                    className={`size-3.5 text-subtle shrink-0 transition-transform duration-150 ${open ? 'rotate-90' : ''}`}
                  />
                  <span className="font-mono text-xs text-accent flex-1 text-left">
                    {group.resourceType}
                  </span>
                  {selectedInGroup.length > 0 && (
                    <span className="text-[0.6875rem] text-accent font-medium">
                      {selectedInGroup.length} selected
                    </span>
                  )}
                  <span className="text-[0.6875rem] text-subtle">
                    {group.scopes.length} {group.scopes.length === 1 ? 'scope' : 'scopes'}
                  </span>
                </button>

                {/* Group body */}
                {open && (
                  <section className="bg-surface-elevated">
                    {/* Select all */}
                    {group.scopes.length > 1 && (
                      <label className="flex items-center gap-2.5 px-3 py-1.5 border-b border-border-subtle cursor-pointer select-none hover:bg-surface-overlay transition-colors">
                        <input
                          type="checkbox"
                          checked={allSelected}
                          ref={(el) => {
                            if (el) el.indeterminate = someSelected;
                          }}
                          onChange={(e) => onToggleGroup(group.scopes, e.target.checked)}
                          className="accent-accent size-3.5 cursor-pointer rounded shrink-0"
                        />
                        <span className="text-xs text-subtle">
                          Select all {group.resourceType} scopes
                        </span>
                      </label>
                    )}

                    {/* Individual scopes */}
                    {group.scopes.map((scope) => (
                      <label
                        key={scope}
                        className="flex items-center gap-2.5 px-3 py-1.5 border-b border-border-subtle last:border-b-0 cursor-pointer select-none hover:bg-surface-overlay transition-colors"
                      >
                        <input
                          type="checkbox"
                          checked={selectedScopes.includes(scope)}
                          onChange={(e) => onToggle(scope, e.target.checked)}
                          className="accent-accent size-3.5 cursor-pointer rounded shrink-0"
                        />
                        <ScopeChip scope={scope} />
                      </label>
                    ))}
                  </section>
                )}
              </section>
            );
          })
        )}
      </section>
    </section>
  );
}
