'use client';

import { useState, useMemo, useCallback } from 'react';
import {
  Heart,
  MoreVertical,
  Search,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  ListFilter,
  Check,
} from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Popover,
  PopoverTrigger,
  PopoverContent,
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
import type { ModelRow, ModelStatus } from '../_types';
import { useModelsContext } from '../_context/models-context';
import { AvailabilityDot } from './availability-dot';
import { ModelDetailModal } from './model-detail-modal';
import { ColumnToggleMenu, ALL_COLUMNS, type ColumnId } from './column-toggle-menu';
import { ModelCardGrid } from './model-card-grid';

// ── Types ─────────────────────────────────────────────────────────────────────

type SortDir = 'asc' | 'desc' | null;
type SortKey = ColumnId | null;

interface FilterState {
  search: string;
  providers: Set<string>;
  statuses: Set<ModelStatus>;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function matchesSearch(row: ModelRow, search: string): boolean {
  if (!search) return true;
  const q = search.toLowerCase();
  return (
    row.owned_by.toLowerCase().includes(q) ||
    row.family.toLowerCase().includes(q) ||
    row.name.toLowerCase().includes(q) ||
    row.id.toLowerCase().includes(q) ||
    row.capabilities.some((c) => c.toLowerCase().includes(q)) ||
    row.dialects.some((d) => d.toLowerCase().includes(q))
  );
}

function sortRows(rows: ModelRow[], key: SortKey, dir: SortDir): ModelRow[] {
  if (!key || !dir) return rows;
  return [...rows].sort((a, b) => {
    let cmp = 0;
    switch (key) {
      case 'name':
        cmp = a.name.localeCompare(b.name);
        break;
      case 'provider':
        cmp = a.owned_by.localeCompare(b.owned_by);
        break;
      case 'family':
        cmp = a.family.localeCompare(b.family);
        break;
      default:
        cmp = 0;
    }
    return dir === 'asc' ? cmp : -cmp;
  });
}

// ── Chips ────────────────────────────────────────────────────────────────────

function Chips({ values }: { values: string[] }) {
  if (values.length === 0)
    return (
      <Text size="caption" color="subtle">
        —
      </Text>
    );
  return (
    <Stack direction="horizontal" wrap gap="tight">
      {values.map((v) => (
        <Badge key={v} variant="outline" size="sm">
          {v}
        </Badge>
      ))}
    </Stack>
  );
}

// ── Search Input ─────────────────────────────────────────────────────────────

function SearchInput({
  query,
  onQueryChange,
}: {
  query: string;
  onQueryChange: (q: string) => void;
}) {
  return (
    <span className="relative flex-1 max-w-xs">
      <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted pointer-events-none" />
      <input
        type="text"
        placeholder="Search models..."
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        className="w-full rounded-md border border-border bg-surface px-3 py-1.5 pl-8 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />
    </span>
  );
}

// ── Sort button ──────────────────────────────────────────────────────────────

interface SortButtonProps {
  label: string;
  columnKey: ColumnId;
  currentKey: SortKey;
  direction: SortDir;
  onSort: (key: ColumnId) => void;
}

function SortButton({ label, columnKey, currentKey, direction, onSort }: SortButtonProps) {
  const active = currentKey === columnKey;
  const Icon =
    active && direction === 'asc'
      ? ArrowUp
      : active && direction === 'desc'
        ? ArrowDown
        : ArrowUpDown;

  return (
    <button
      type="button"
      onClick={() => onSort(columnKey)}
      className={[
        'inline-flex items-center gap-1 rounded px-0.5 py-0.5 transition-colors',
        'hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        active ? 'text-foreground' : 'text-muted-foreground',
      ].join(' ')}
    >
      <span>{label}</span>
      <Icon className="size-3 shrink-0 opacity-60" aria-hidden />
    </button>
  );
}

// ── Filter popover ───────────────────────────────────────────────────────────

interface FilterOption {
  value: string;
  label: string;
  dotClass?: string;
}

interface FilterPopoverProps {
  options: FilterOption[];
  selected: Set<string>;
  onToggle: (value: string) => void;
  onClear: () => void;
}

function FilterPopover({ options, selected, onToggle, onClear }: FilterPopoverProps) {
  const active = selected.size > 0;
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Filter"
          className={[
            'relative inline-flex items-center justify-center rounded p-0.5 transition-colors',
            'hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
            active ? 'text-accent' : 'text-muted-foreground hover:text-foreground',
          ].join(' ')}
        >
          <ListFilter className="size-3" aria-hidden />
          {active && (
            <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-accent" />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent>
        <Stack asChild gap="none">
          <menu className="w-44 p-1 list-none m-0">
            {options.map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => onToggle(opt.value)}
                className={[
                  'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs transition-colors',
                  'hover:bg-surface focus-visible:outline-2 focus-visible:outline-accent',
                  selected.has(opt.value) ? 'text-foreground' : 'text-muted-foreground',
                ].join(' ')}
              >
                <span className="flex size-3.5 shrink-0 items-center justify-center rounded border border-border">
                  {selected.has(opt.value) && <Check className="size-2.5 text-accent" />}
                </span>
                {opt.dotClass && (
                  <span className={`size-1.5 shrink-0 rounded-full ${opt.dotClass}`} />
                )}
                {opt.label}
              </button>
            ))}
            {active && (
              <>
                <hr className="my-1 border-border" />
                <button
                  type="button"
                  onClick={onClear}
                  className="flex w-full items-center rounded px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-surface hover:text-foreground focus-visible:outline-2 focus-visible:outline-accent"
                >
                  Clear filter
                </button>
              </>
            )}
          </menu>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

// ── Constants ────────────────────────────────────────────────────────────────

const DEFAULT_VISIBLE: Set<ColumnId> = new Set([
  'favourite',
  'status',
  'provider',
  'family',
  'name',
  'capabilities',
  'dialects',
]);

const STATUS_OPTIONS: FilterOption[] = [
  { value: 'available', label: 'Available', dotClass: 'bg-status-healthy' },
  { value: 'unavailable', label: 'Unavailable', dotClass: 'bg-status-error' },
];

// ── ModelsTab ────────────────────────────────────────────────────────────────

export function ModelsTab() {
  const { models, status, favourites, toggleFavourite, refresh } = useModelsContext();

  const favouriteRows = useMemo(
    () => models.filter((m) => favourites.has(m.id)),
    [models, favourites],
  );
  const [filters, setFilters] = useState<FilterState>({
    search: '',
    providers: new Set(),
    statuses: new Set(),
  });
  const [visible, setVisible] = useState<Set<ColumnId>>(DEFAULT_VISIBLE);
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');
  const [selectedModel, setSelectedModel] = useState<ModelRow | null>(null);

  const PROVIDERS = useMemo(() => Array.from(new Set(models.map((m) => m.owned_by))), [models]);
  const providerOptions: FilterOption[] = PROVIDERS.map((p) => ({ value: p, label: p }));

  const handleSort = useCallback((key: ColumnId) => {
    setSortKey((prev) => {
      if (prev === key) {
        setSortDir((d) => (d === 'asc' ? 'desc' : d === 'desc' ? null : 'asc'));
        return prev;
      }
      setSortDir('asc');
      return key;
    });
  }, []);

  const toggleColumn = useCallback((id: ColumnId) => {
    setVisible((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleProvider = useCallback((p: string) => {
    setFilters((prev) => {
      const next = new Set(prev.providers);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return { ...prev, providers: next };
    });
  }, []);

  const toggleStatus = useCallback((s: string) => {
    setFilters((prev) => {
      const next = new Set(prev.statuses);
      if (next.has(s as ModelStatus)) next.delete(s as ModelStatus);
      else next.add(s as ModelStatus);
      return { ...prev, statuses: next };
    });
  }, []);

  const filteredRows = useMemo(() => {
    const rows = models.filter((row) => {
      if (filters.providers.size > 0 && !filters.providers.has(row.owned_by)) return false;
      if (filters.statuses.size > 0 && !filters.statuses.has(row.status)) return false;
      if (!matchesSearch(row, filters.search)) return false;
      return true;
    });
    return sortRows(rows, sortKey, sortDir);
  }, [models, filters, sortKey, sortDir]);

  function col(id: ColumnId) {
    return visible.has(id);
  }

  const visibleCount = ALL_COLUMNS.filter((c) => visible.has(c.id)).length;

  return (
    <Stack gap="comfortable">
      {/* Favourites card grid */}
      <ModelCardGrid
        models={favouriteRows}
        onRemove={toggleFavourite}
        onSelect={setSelectedModel}
      />

      {/* Toolbar */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <SearchInput
          query={filters.search}
          onQueryChange={(q) => setFilters((prev) => ({ ...prev, search: q }))}
        />
        <span className="flex-1" />
        <Text size="caption" color="muted">
          {filteredRows.length} of {models.length} models
        </Text>
        <ColumnToggleMenu visible={visible} onToggle={toggleColumn} />
      </Stack>

      {/* Table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  {col('favourite') && (
                    <TableHead>
                      <span className="w-10 block" />
                    </TableHead>
                  )}

                  {col('name') && (
                    <TableHead>
                      <SortButton
                        label="Model"
                        columnKey="name"
                        currentKey={sortKey}
                        direction={sortDir}
                        onSort={handleSort}
                      />
                    </TableHead>
                  )}

                  {col('provider') && (
                    <TableHead>
                      <Stack direction="horizontal" align="center" gap="tight">
                        <SortButton
                          label="Provider"
                          columnKey="provider"
                          currentKey={sortKey}
                          direction={sortDir}
                          onSort={handleSort}
                        />
                        <FilterPopover
                          options={providerOptions}
                          selected={filters.providers as Set<string>}
                          onToggle={toggleProvider}
                          onClear={() => setFilters((prev) => ({ ...prev, providers: new Set() }))}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('family') && (
                    <TableHead>
                      <SortButton
                        label="Family"
                        columnKey="family"
                        currentKey={sortKey}
                        direction={sortDir}
                        onSort={handleSort}
                      />
                    </TableHead>
                  )}

                  {col('status') && (
                    <TableHead>
                      <Stack direction="horizontal" align="center" gap="tight">
                        <span>Status</span>
                        <FilterPopover
                          options={STATUS_OPTIONS}
                          selected={filters.statuses as Set<string>}
                          onToggle={toggleStatus}
                          onClear={() => setFilters((prev) => ({ ...prev, statuses: new Set() }))}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('capabilities') && <TableHead>Capabilities</TableHead>}
                  {col('dialects') && <TableHead>Dialects</TableHead>}
                  {col('inputModalities') && <TableHead>Input</TableHead>}
                  {col('outputModalities') && <TableHead>Output</TableHead>}

                  <TableHead>
                    <span className="w-10 block" />
                  </TableHead>
                </TableRow>
              </TableHeader>

              <TableBody>
                {status === 'error' ? (
                  <TableRow>
                    <TableCell colSpan={visibleCount + 1}>
                      <Stack align="center" justify="center">
                        <figure className="py-12 text-center m-0">
                          <Stack gap="snug" align="center">
                            <Text size="supporting" color="muted">
                              Couldn&apos;t load models. The inference gateway may be unavailable.
                            </Text>
                            <button
                              type="button"
                              onClick={() => void refresh()}
                              className="rounded-md border border-border px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-accent"
                            >
                              Try again
                            </button>
                          </Stack>
                        </figure>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ) : filteredRows.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={visibleCount + 1}>
                      <Stack align="center" justify="center">
                        <figure className="py-8 text-center m-0">
                          <Text size="supporting" color="muted">
                            {filters.search ||
                            filters.providers.size > 0 ||
                            filters.statuses.size > 0
                              ? 'No models match your filters.'
                              : 'No models available.'}
                          </Text>
                        </figure>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredRows.map((row) => {
                    const isFav = favourites.has(row.id);
                    return (
                      <TableRow
                        key={row.id}
                        style={{ cursor: 'pointer' }}
                        onClick={() => setSelectedModel(row)}
                      >
                        {col('favourite') && (
                          <TableCell>
                            <button
                              type="button"
                              aria-label={isFav ? 'Remove from favourites' : 'Add to favourites'}
                              onClick={(e) => {
                                e.stopPropagation();
                                toggleFavourite(row.id);
                              }}
                              className="inline-flex items-center justify-center rounded p-1 transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-accent"
                            >
                              <Heart
                                className={[
                                  'size-3.5 transition-colors',
                                  isFav
                                    ? 'fill-rose-500 text-rose-500'
                                    : 'text-muted-foreground opacity-30 hover:opacity-100',
                                ].join(' ')}
                              />
                            </button>
                          </TableCell>
                        )}

                        {col('name') && (
                          <TableCell>
                            <Text size="supporting" weight="emphasis" mono>
                              {row.id}
                            </Text>
                          </TableCell>
                        )}

                        {col('provider') && (
                          <TableCell>
                            <Text size="supporting" color="muted">
                              {row.owned_by}
                            </Text>
                          </TableCell>
                        )}

                        {col('family') && (
                          <TableCell>
                            <Text size="supporting" color="muted">
                              {row.family || '—'}
                            </Text>
                          </TableCell>
                        )}

                        {col('status') && (
                          <TableCell>
                            <Stack direction="horizontal" align="center" gap="tight">
                              <AvailabilityDot status={row.status} />
                              <Text size="supporting" color="muted">
                                {row.status}
                              </Text>
                            </Stack>
                          </TableCell>
                        )}

                        {col('capabilities') && (
                          <TableCell>
                            <Chips values={row.capabilities} />
                          </TableCell>
                        )}

                        {col('dialects') && (
                          <TableCell>
                            <Chips values={row.dialects} />
                          </TableCell>
                        )}

                        {col('inputModalities') && (
                          <TableCell>
                            <Chips values={row.input_modalities} />
                          </TableCell>
                        )}

                        {col('outputModalities') && (
                          <TableCell>
                            <Chips values={row.output_modalities} />
                          </TableCell>
                        )}

                        {/* Actions */}
                        <TableCell>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="View model details"
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelectedModel(row);
                            }}
                          >
                            <MoreVertical className="size-3.5" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>

      <ModelDetailModal model={selectedModel} onClose={() => setSelectedModel(null)} />
    </Stack>
  );
}
