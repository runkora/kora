'use client';

import { useMemo, useState } from 'react';
import {
  Badge,
  Card,
  CardContent,
  Heading,
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
import { Route, Search } from 'lucide-react';
import type { RouteRegistration } from '../_types';
import { ScopeChip } from './scope-chip';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

interface RoutesTabProps {
  initialRegistrations: RouteRegistration[];
  initialError: string | null;
}

interface FlatRoute {
  service: string;
  tenantId: string;
  pathPrefix: string;
  method: string;
  requiredScope: string;
}

const METHOD_COLORS: Record<string, string> = {
  GET: 'text-status-healthy',
  POST: 'text-accent',
  PUT: 'text-status-degraded',
  PATCH: 'text-status-degraded',
  DELETE: 'text-status-error',
};

export function RoutesTab({ initialRegistrations, initialError }: RoutesTabProps) {
  const [search, setSearch] = useState('');
  const [serviceFilter, setServiceFilter] = useState('');
  const [methodFilter, setMethodFilter] = useState('');

  const flatRoutes = useMemo(() => {
    const rows: FlatRoute[] = [];
    for (const reg of initialRegistrations) {
      for (const route of reg.routes) {
        rows.push({
          service: reg.service,
          tenantId: reg.tenant_id || 'global',
          pathPrefix: route.path_prefix,
          method: route.method,
          requiredScope: route.required_scope,
        });
      }
    }
    return rows;
  }, [initialRegistrations]);

  const services = useMemo(
    () => Array.from(new Set(flatRoutes.map((r) => r.service))).sort(),
    [flatRoutes],
  );

  const methods = useMemo(
    () => Array.from(new Set(flatRoutes.map((r) => r.method))).sort(),
    [flatRoutes],
  );

  const filtered = useMemo(() => {
    let result = flatRoutes;
    if (serviceFilter) {
      result = result.filter((r) => r.service === serviceFilter);
    }
    if (methodFilter) {
      result = result.filter((r) => r.method === methodFilter);
    }
    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (r) =>
          r.pathPrefix.toLowerCase().includes(q) ||
          r.requiredScope.toLowerCase().includes(q) ||
          r.service.toLowerCase().includes(q),
      );
    }
    return result;
  }, [flatRoutes, search, serviceFilter, methodFilter]);

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Routes</Heading>
        <Text size="supporting" color="muted">
          Registered route→scope mappings across all services.
        </Text>
      </Stack>

      {/* Info box */}
      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Services register their route→scope mappings at startup. This is the source of truth for
          what scope is required to call each endpoint — compiled into OPA bundles and enforced by
          Envoy sidecars.
        </Text>
      </section>

      {initialError && <ErrorBanner message={initialError} />}

      {/* Filters */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <span className="relative flex-1 max-w-xs">
          <input
            type="text"
            placeholder="Search routes..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
        </span>
        <select
          value={serviceFilter}
          onChange={(e) => setServiceFilter(e.target.value)}
          aria-label="Filter by service"
          className="h-8 rounded-md border border-border bg-surface px-3 text-sm text-foreground"
        >
          <option value="">All services</option>
          {services.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <select
          value={methodFilter}
          onChange={(e) => setMethodFilter(e.target.value)}
          aria-label="Filter by method"
          className="h-8 rounded-md border border-border bg-surface px-3 text-sm text-foreground"
        >
          <option value="">All methods</option>
          {methods.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
        <Text size="caption" color="subtle">
          {filtered.length} routes
        </Text>
      </Stack>

      {/* Content */}
      {filtered.length === 0 ? (
        search || serviceFilter || methodFilter ? (
          <EmptyState
            icon={<Search className="size-5" />}
            title="No matches"
            description="No routes match your current filters. Try adjusting your search or clearing the service/method filter."
          />
        ) : (
          <EmptyState
            icon={<Route className="size-5" />}
            title="No routes registered"
            description="Routes are registered by services at startup. Once a service declares its route→scope mappings, they will appear here."
          />
        )
      ) : (
        <Card column>
          <CardContent flush>
            <TableRoot>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Service</TableHead>
                    <TableHead>Method</TableHead>
                    <TableHead>Path Prefix</TableHead>
                    <TableHead>Required Scope</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map((route) => (
                    <TableRow
                      key={`${route.service}-${route.method}-${route.pathPrefix}-${route.requiredScope}`}
                    >
                      <TableCell>
                        <Badge variant="secondary" size="sm">
                          {route.service}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <span
                          className={`font-mono text-xs ${METHOD_COLORS[route.method] ?? 'text-muted'}`}
                        >
                          {route.method}
                        </span>
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs text-subtle">{route.pathPrefix}</span>
                      </TableCell>
                      <TableCell>
                        <ScopeChip scope={route.requiredScope} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableRoot>
          </CardContent>
          <footer className="px-4 py-2.5 border-t border-border-subtle">
            <Text size="caption" color="muted">
              {filtered.length} {filtered.length === 1 ? 'registration' : 'registrations'} ·{' '}
              {services.length} {services.length === 1 ? 'service' : 'services'} · recompiled into
              OPA bundles on every change
            </Text>
          </footer>
        </Card>
      )}
    </Stack>
  );
}
