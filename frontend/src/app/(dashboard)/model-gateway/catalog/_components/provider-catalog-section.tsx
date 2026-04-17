'use client';

import { useState, useCallback } from 'react';
import { CircleDot, Search } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  Input,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  Text,
  Toggle,
} from '@orbit/terra';
import type { ProviderModel, Provider } from '../_types/catalog';
import { PROVIDER_LABELS } from '../_utils/catalog-constants';
import { ProviderBadge } from './provider-badge';

interface ProviderCatalogSectionProps {
  models: ProviderModel[];
}

export function ProviderCatalogSection({ models }: ProviderCatalogSectionProps) {
  const [search, setSearch] = useState('');
  const [providerFilter, setProviderFilter] = useState<Provider | 'all'>('all');
  const [hideDisabled, setHideDisabled] = useState(false);
  const [localModels, setLocalModels] = useState(models);

  const filtered = localModels.filter((m) => {
    if (hideDisabled && !m.enabled) return false;
    if (providerFilter !== 'all' && m.provider !== providerFilter) return false;
    if (search) {
      const q = search.toLowerCase();
      if (
        !m.displayName.toLowerCase().includes(q) &&
        !m.family.toLowerCase().includes(q) &&
        !m.modelId.toLowerCase().includes(q)
      )
        return false;
    }
    return true;
  });

  const handleSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setSearch(e.target.value);
  }, []);

  const toggle = useCallback(async (provider: string, modelId: string, enabled: boolean) => {
    setLocalModels((prev) =>
      prev.map((m) => (m.provider === provider && m.modelId === modelId ? { ...m, enabled } : m)),
    );
    try {
      const res = await fetch(
        `/api/catalog/provider-models/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled }),
        },
      );
      if (!res.ok) {
        setLocalModels((prev) =>
          prev.map((m) =>
            m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
          ),
        );
      }
    } catch {
      setLocalModels((prev) =>
        prev.map((m) =>
          m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
        ),
      );
    }
  }, []);

  const providers: Provider[] = ['bedrock', 'vertexai', 'azure'];

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <span className="relative max-w-xs flex-1 [&>input]:pl-9">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
          <Input value={search} onChange={handleSearchChange} placeholder="Filter models…" />
        </span>

        <Stack direction="horizontal" gap="tight">
          <Button
            variant={providerFilter === 'all' ? 'secondary' : 'ghost'}
            size="sm"
            onClick={() => setProviderFilter('all')}
          >
            All
          </Button>
          {providers.map((p) => (
            <Button
              key={p}
              variant={providerFilter === p ? 'secondary' : 'ghost'}
              size="sm"
              onClick={() => setProviderFilter(providerFilter === p ? 'all' : p)}
            >
              {PROVIDER_LABELS[p]}
            </Button>
          ))}
        </Stack>

        <Button
          variant={hideDisabled ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setHideDisabled(!hideDisabled)}
        >
          {hideDisabled ? 'Show all' : 'Hide disabled'}
        </Button>
      </Stack>

      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Model ID</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Family</TableHead>
                  <TableHead>Input</TableHead>
                  <TableHead>Output</TableHead>
                  <TableHead>Dialects</TableHead>
                  <TableHead>Capabilities</TableHead>
                  <TableHead>Enabled</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {localModels.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8}>
                      <figure className="py-12 text-center m-0">
                        <Stack gap="snug" align="center">
                          <CircleDot className="size-8 text-muted" />
                          <Text size="supporting" color="muted">
                            No provider models discovered yet
                          </Text>
                          <Text size="caption" color="muted">
                            Provider models appear here after the gateway performs its first
                            discovery sweep.
                          </Text>
                        </Stack>
                      </figure>
                    </TableCell>
                  </TableRow>
                ) : (
                  filtered.map((m) => (
                    <TableRow
                      key={`${m.provider}/${m.modelId}`}
                      style={{ opacity: m.enabled ? 1 : 0.5 }}
                    >
                      <TableCell>
                        <Stack gap="none">
                          <Text size="supporting" weight={m.enabled ? 'emphasis' : 'regular'} mono>
                            {m.modelId}
                          </Text>
                          {m.displayName && (
                            <Text size="caption" color="muted">
                              {m.displayName}
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <ProviderBadge provider={m.provider} />
                      </TableCell>
                      <TableCell>
                        <Text size="supporting" color="muted" mono>
                          {m.family || '—'}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.inputModalities.length > 0 ? (
                            m.inputModalities.map((mod) => (
                              <Badge key={mod} variant="outline" size="sm">
                                {mod}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.outputModalities.length > 0 ? (
                            m.outputModalities.map((mod) => (
                              <Badge key={mod} variant="outline" size="sm">
                                {mod}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.dialects.length > 0 ? (
                            m.dialects.map((d) => (
                              <Badge key={d} variant="outline" size="sm">
                                {d}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Stack direction="horizontal" wrap gap="tight">
                          {m.capabilities.length > 0 ? (
                            m.capabilities.map((c) => (
                              <Badge key={c} variant="outline" size="sm">
                                {c}
                              </Badge>
                            ))
                          ) : (
                            <Text size="caption" color="muted">
                              —
                            </Text>
                          )}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Toggle
                          checked={m.enabled}
                          onChange={(v) => void toggle(m.provider, m.modelId, v)}
                        />
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
        {localModels.length > 0 && (
          <CardFooter compact justify="between">
            <Text size="caption" color="muted">
              {localModels.filter((m) => m.enabled).length} of {localModels.length} provider models
              enabled
            </Text>
            <Text size="caption" color="muted">
              Disabled models are excluded from routing
            </Text>
          </CardFooter>
        )}
      </Card>
    </Stack>
  );
}
