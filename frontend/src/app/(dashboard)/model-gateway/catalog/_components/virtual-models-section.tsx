'use client';

import { useState } from 'react';
import { ChevronRight, Layers, Plus, Search } from 'lucide-react';
import {
  Button,
  Card,
  CardContent,
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
} from '@orbit/terra';
import type { VirtualModel } from '../_types/catalog';
import { PROVIDER_LABELS } from '../_utils/catalog-constants';
import { StrategyBadge } from './strategy-badge';

interface VirtualModelsSectionProps {
  models: VirtualModel[];
  onSelect: (m: VirtualModel) => void;
}

export function VirtualModelsSection({ models, onSelect }: VirtualModelsSectionProps) {
  const [search, setSearch] = useState('');

  const filtered = models.filter((m) => {
    if (!search) return true;
    const q = search.toLowerCase();
    return m.family.includes(q) || m.name.includes(q) || m.displayName.toLowerCase().includes(q);
  });

  const families = Array.from(new Set(filtered.map((m) => m.family))).sort();

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" gap="snug">
        <span className="relative flex-1 max-w-xs [&>input]:pl-9">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search virtual models…"
          />
        </span>
        <Button
          size="sm"
          onClick={() =>
            onSelect({
              id: '__new__',
              family: '',
              name: '',
              displayName: '',
              description: '',
              strategy: 'priority',
              routes: [],
              enabled: true,
            })
          }
        >
          <Plus className="mr-1.5 size-3.5" />
          New Virtual Model
        </Button>
      </Stack>

      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Virtual Model</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Routes</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {models.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5}>
                      <figure className="py-12 text-center m-0">
                        <Stack gap="snug" align="center">
                          <Layers className="size-8 text-muted" />
                          <Text size="supporting" color="muted">
                            No virtual models configured
                          </Text>
                          <Text size="caption" color="muted">
                            Create your first virtual model to start routing inference requests.
                          </Text>
                        </Stack>
                      </figure>
                    </TableCell>
                  </TableRow>
                ) : (
                  families.map((family) =>
                    filtered
                      .filter((m) => m.family === family)
                      .map((m) => (
                        <TableRow
                          key={m.id}
                          style={{ cursor: 'pointer' }}
                          onClick={() => onSelect(m)}
                        >
                          <TableCell>
                            <Stack gap="none">
                              <span className="mb-0.5 block text-[10px] uppercase tracking-wider">
                                <Text size="caption" color="subtle">
                                  {family}
                                </Text>
                              </span>
                              <Text size="supporting" weight="emphasis" mono>
                                {m.name}
                              </Text>
                              <Text size="caption" color="muted">
                                {m.displayName}
                              </Text>
                            </Stack>
                          </TableCell>
                          <TableCell>
                            <StrategyBadge strategy={m.strategy} />
                          </TableCell>
                          <TableCell>
                            <Stack direction="horizontal" align="center" gap="tight">
                              <Text size="supporting">{m.routes.length}</Text>
                              <span className="flex gap-0.5">
                                {m.routes.map((r, ri) => (
                                  <span
                                    key={r.id || ri}
                                    className={`h-1.5 w-1.5 rounded-full ${r.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
                                    title={`${PROVIDER_LABELS[r.provider] ?? r.provider} — ${r.enabled ? 'active' : 'disabled'}`}
                                  />
                                ))}
                              </span>
                            </Stack>
                          </TableCell>
                          <TableCell>
                            <span
                              className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
                                m.enabled
                                  ? 'bg-status-healthy/15 text-status-healthy'
                                  : 'bg-muted/20 text-muted'
                              }`}
                            >
                              <span
                                className={`h-1.5 w-1.5 rounded-full ${m.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
                              />
                              {m.enabled ? 'Enabled' : 'Disabled'}
                            </span>
                          </TableCell>
                          <TableCell>
                            <ChevronRight className="size-4 text-muted" />
                          </TableCell>
                        </TableRow>
                      )),
                  )
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>
    </Stack>
  );
}
