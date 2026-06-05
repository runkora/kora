'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Callout,
  Alert,
  Badge,
  Button,
  Code,
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
import { ScopeChip } from '@orbit/components';
import { EmptyState } from '@orbit/components';
import { FilterChip } from './filter-chip';

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
      <Callout>
        <Text size="caption" color="muted">
          Scopes use the format <Code>resource:path:action</Code>. Sibling scopes are independent —{' '}
          <Code>mcp:github:read</Code> does not grant <Code>mcp:github:write</Code>
        </Text>
      </Callout>

      {initialError && (
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Text size="caption" color="status-error">
            {initialError}
          </Text>
        </Alert>
      )}

      {/* Search */}
      <Stack asChild maxWidth="compact">
        <Input
          type="text"
          placeholder="Filter scopes..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </Stack>

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
          <Stack key={group.resourceType}>
            {/* Group header */}
            <Stack direction="horizontal" align="center" gap="snug" marginBottom="snug">
              <Text size="supporting" color="accent" mono weight="strong">
                {group.resourceType}
              </Text>
              <Text size="caption" color="subtle">
                {group.scopes.length} {group.scopes.length === 1 ? 'scope' : 'scopes'}
              </Text>
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
          </Stack>
        ))
      )}
    </Stack>
  );
}

// ── Usage Pill ──────────────────────────────────────────────────────────────

function UsagePill({ count, label }: { count: number; label: string }) {
  return (
    <Badge variant={count > 0 ? 'elevated' : 'muted'} size="sm">
      {count} {label}
    </Badge>
  );
}
