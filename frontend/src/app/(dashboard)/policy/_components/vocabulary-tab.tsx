'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Icon,
  Card,
  CardContent,
  Heading,
  Input,
  Stack,
  Text,
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@orbit/terra';
import type { ScopeEntryWithUsage } from '../_types';
import { ScopeChip } from './scope-chip';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

interface VocabularyTabProps {
  initialScopes: ScopeEntryWithUsage[];
  initialError: string | null;
}

interface ScopeGroup {
  resourceType: string;
  scopes: ScopeEntryWithUsage[];
}

function groupScopes(scopes: ScopeEntryWithUsage[]): ScopeGroup[] {
  const map = new Map<string, ScopeEntryWithUsage[]>();
  for (const scope of scopes) {
    if (!map.has(scope.resource_type)) map.set(scope.resource_type, []);
    map.get(scope.resource_type)!.push(scope);
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([resourceType, groupScopes]) => ({ resourceType, scopes: groupScopes }));
}

export function VocabularyTab({ initialScopes, initialError }: VocabularyTabProps) {
  const [search, setSearch] = useState('');
  const [activeFilter, setActiveFilter] = useState('all');
  const [copiedScope, setCopiedScope] = useState<string | null>(null);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout>>(null);

  // Clean up copy timer on unmount
  useEffect(() => {
    return () => {
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
    };
  }, []);

  const resourceTypes = useMemo(() => {
    const counts = new Map<string, number>();
    for (const s of initialScopes) {
      counts.set(s.resource_type, (counts.get(s.resource_type) ?? 0) + 1);
    }
    return Array.from(counts.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [initialScopes]);

  const filtered = useMemo(() => {
    let result = initialScopes;
    if (activeFilter !== 'all') {
      result = result.filter((s) => s.resource_type === activeFilter);
    }
    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (s) =>
          s.scope.toLowerCase().includes(q) || (s.description?.toLowerCase().includes(q) ?? false),
      );
    }
    return result;
  }, [initialScopes, search, activeFilter]);

  const groups = useMemo(() => groupScopes(filtered), [filtered]);

  const handleCopy = useCallback((scope: string) => {
    navigator.clipboard.writeText(scope);
    setCopiedScope(scope);
    if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
    copyTimerRef.current = setTimeout(() => setCopiedScope(null), 1500);
  }, []);

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Vocabulary</Heading>
        <Text size="supporting" color="muted">
          Registered scope strings that grants and roles can reference.
        </Text>
      </Stack>

      {/* Info box */}
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Scopes use the format{' '}
          <code className="font-mono text-[0.6875rem] bg-surface-overlay border border-border-subtle rounded px-1 py-0.5">
            resource:path:action
          </code>
          . Sibling scopes are independent —{' '}
          <code className="font-mono text-[0.6875rem] bg-surface-overlay border border-border-subtle rounded px-1 py-0.5">
            mcp:github:read
          </code>{' '}
          does not grant{' '}
          <code className="font-mono text-[0.6875rem] bg-surface-overlay border border-border-subtle rounded px-1 py-0.5">
            mcp:github:write
          </code>
        </Text>
      </section>

      {initialError && <ErrorBanner message={initialError} />}

      {/* Search */}
      <Input
        type="text"
        placeholder="Filter scopes..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="max-w-xs"
      />

      {/* Filter chips */}
      <Stack direction="horizontal" align="center" justify="between">
        <Stack direction="horizontal" align="center" gap="snug" wrap>
          <FilterChip
            label="All"
            count={initialScopes.length}
            active={activeFilter === 'all'}
            onClick={() => setActiveFilter('all')}
          />
          {resourceTypes.map(([type, count]) => (
            <FilterChip
              key={type}
              label={type}
              count={count}
              mono
              active={activeFilter === type}
              onClick={() => setActiveFilter(type)}
            />
          ))}
        </Stack>
        <Text size="caption" color="subtle">
          {filtered.length} of {initialScopes.length} scopes
        </Text>
      </Stack>

      {/* Grouped tables */}
      {groups.length === 0 ? (
        search || activeFilter !== 'all' ? (
          <EmptyState
            icon="search"
            title="No matches"
            description="No scopes match your current filters. Try adjusting your search or selecting a different resource type."
          />
        ) : (
          <EmptyState
            icon="library"
            title="No scopes registered"
            description="Scopes are declared by services when they register their routes. Once a service registers its route→scope mappings, they will appear here."
          />
        )
      ) : (
        groups.map((group) => (
          <section key={group.resourceType}>
            {/* Group header */}
            <Stack direction="horizontal" align="center" gap="snug" asChild>
              <header className="mb-3">
                <Text size="supporting" color="accent" mono weight="strong">
                  {group.resourceType}
                </Text>
                <Text size="caption" color="subtle">
                  {group.scopes.length} {group.scopes.length === 1 ? 'scope' : 'scopes'}
                </Text>
              </header>
            </Stack>

            {/* Group table */}
            <Card column>
              <CardContent flush>
                <TableRoot>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Scope</TableHead>
                        <TableHead>Description</TableHead>
                        <TableHead align="right">Used in</TableHead>
                        <TableHead srOnly>Copy</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {group.scopes.map((scope) => (
                        <TableRow key={scope.scope}>
                          <TableCell>
                            <ScopeChip scope={scope.scope} />
                          </TableCell>
                          <TableCell>
                            <Text size="caption" color="muted">
                              {scope.description || '—'}
                            </Text>
                          </TableCell>
                          <TableCell>
                            <Stack direction="horizontal" align="center" justify="end" gap="tight">
                              <UsagePill
                                count={scope.grant_count}
                                label={scope.grant_count === 1 ? 'grant' : 'grants'}
                              />
                              <UsagePill
                                count={scope.role_count}
                                label={scope.role_count === 1 ? 'role' : 'roles'}
                              />
                            </Stack>
                          </TableCell>
                          <TableCell>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => handleCopy(scope.scope)}
                              aria-label={`Copy ${scope.scope}`}
                            >
                              {copiedScope === scope.scope ? (
                                <Icon name="check" size="2xs" color="status-healthy" />
                              ) : (
                                <Icon name="copy" size="2xs" color="subtle" />
                              )}
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </TableRoot>
              </CardContent>
            </Card>
          </section>
        ))
      )}
    </Stack>
  );
}

// ── Filter Chip ─────────────────────────────────────────────────────────────

function FilterChip({
  label,
  count,
  mono,
  active,
  onClick,
}: {
  label: string;
  count: number;
  mono?: boolean;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border transition-colors select-none cursor-pointer',
        active
          ? 'bg-accent/10 text-accent border-accent/30'
          : 'bg-transparent text-muted border-border hover:bg-surface-elevated hover:text-foreground',
      ].join(' ')}
    >
      {mono ? (
        <Text asChild size="micro" mono>
          <output>{label}</output>
        </Text>
      ) : (
        label
      )}
      <Text asChild size="micro" color="muted">
        <output>{count}</output>
      </Text>
    </button>
  );
}

// ── Usage Pill ──────────────────────────────────────────────────────────────

function UsagePill({ count, label }: { count: number; label: string }) {
  return (
    <div
      className={[
        'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[0.6875rem] font-medium',
        count > 0 ? 'bg-surface-elevated text-foreground' : 'bg-transparent text-subtle',
      ].join(' ')}
    >
      {count} {label}
    </div>
  );
}
