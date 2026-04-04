'use client';

import { useState, useMemo, useCallback } from 'react';
import { Heart, ArrowUp, ArrowDown, ArrowUpDown, ListFilter, Check } from 'lucide-react';
import {
  Badge,
  Text,
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  Popover,
  PopoverTrigger,
  PopoverContent,
} from '@orbit/terra';
import type { ModelRow, ModelStatus } from '../../_types';
import { useModelsContext } from '../../_context/models-context';
import { AvailabilityDot } from './availability-dot';
import { ModelDetailModal } from './model-detail-modal';
import { ColumnToggleMenu, ALL_COLUMNS, type ColumnId } from './column-toggle-menu';

// ── Types ─────────────────────────────────────────────────────────────────────

type SortDir = 'asc' | 'desc' | null;
type SortKey = ColumnId | null;

export interface FilterState {
  search: string;
  providers: Set<string>;
  statuses: Set<ModelStatus>;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function matchesSearch(row: ModelRow, search: string) {
  if (!search) return true;
  const q = search.toLowerCase();
  return (
    row.owned_by.toLowerCase().includes(q) ||
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
      default:
        cmp = 0;
    }
    return dir === 'asc' ? cmp : -cmp;
  });
}

// ── Sort button ───────────────────────────────────────────────────────────────

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

// ── Filter popover ─────────────────────────────────────────────────────────────

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
        <div className="w-44 p-1">
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
              <div className="my-1 h-px bg-border" />
              <button
                type="button"
                onClick={onClear}
                className="flex w-full items-center rounded px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-surface hover:text-foreground focus-visible:outline-2 focus-visible:outline-accent"
              >
                Clear filter
              </button>
            </>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ── ModelsTable ───────────────────────────────────────────────────────────────

const DEFAULT_VISIBLE: Set<ColumnId> = new Set([
  'favourite',
  'status',
  'provider',
  'name',
  'capabilities',
  'dialects',
]);

const STATUS_OPTIONS: FilterOption[] = [
  { value: 'available', label: 'Available', dotClass: 'bg-status-healthy' },
  { value: 'unavailable', label: 'Unavailable', dotClass: 'bg-status-error' },
];

// Chip component for rendering small badge-style tags in cells
function Chips({ values }: { values: string[] }) {
  if (values.length === 0)
    return (
      <Text size="caption" color="subtle">
        —
      </Text>
    );
  return (
    <div className="flex flex-wrap gap-1">
      {values.map((v) => (
        <Badge key={v} variant="outline" size="sm">
          {v}
        </Badge>
      ))}
    </div>
  );
}

export function ModelsTable() {
  const { models, favourites, toggleFavourite } = useModelsContext();
  const [filters, setFilters] = useState<FilterState>({
    search: '',
    providers: new Set(),
    statuses: new Set(),
  });
  const [visible, setVisible] = useState<Set<ColumnId>>(DEFAULT_VISIBLE);
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');
  const [selectedModel, setSelectedModel] = useState<ModelRow | null>(null);
  const [hoveredRow, setHoveredRow] = useState<string | null>(null);

  const PROVIDERS = useMemo(() => Array.from(new Set(models.map((m) => m.owned_by))), [models]);

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
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }, []);

  const toggleProvider = useCallback((p: string) => {
    setFilters((prev) => {
      const next = new Set(prev.providers);
      next.has(p) ? next.delete(p) : next.add(p);
      return { ...prev, providers: next };
    });
  }, []);

  const toggleStatus = useCallback((s: string) => {
    setFilters((prev) => {
      const next = new Set(prev.statuses);
      next.has(s as ModelStatus) ? next.delete(s as ModelStatus) : next.add(s as ModelStatus);
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

  const providerOptions: FilterOption[] = PROVIDERS.map((p) => ({ value: p, label: p }));

  return (
    <>
      {/* Toolbar */}
      <div className="flex items-center justify-between gap-3">
        <Text size="supporting" color="muted">
          {filteredRows.length} of {models.length} models
        </Text>
        <div className="flex items-center gap-2">
          <input
            type="search"
            placeholder="Search…"
            value={filters.search}
            onChange={(e) => setFilters((prev) => ({ ...prev, search: e.target.value }))}
            className="h-8 w-72 rounded-md border border-border bg-surface-elevated px-3 text-xs text-foreground placeholder:text-muted-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
          <ColumnToggleMenu visible={visible} onToggle={toggleColumn} />
        </div>
      </div>

      {/* Table */}
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              {col('favourite') && <TableHead />}

              {col('status') && (
                <TableHead>
                  <div className="flex items-center justify-center gap-1">
                    <span>Status</span>
                    <FilterPopover
                      options={STATUS_OPTIONS}
                      selected={filters.statuses as Set<string>}
                      onToggle={toggleStatus}
                      onClear={() => setFilters((prev) => ({ ...prev, statuses: new Set() }))}
                    />
                  </div>
                </TableHead>
              )}

              {col('provider') && (
                <TableHead>
                  <div className="flex items-center gap-1">
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
                  </div>
                </TableHead>
              )}

              {col('name') && (
                <TableHead>
                  <SortButton
                    label="Model Name"
                    columnKey="name"
                    currentKey={sortKey}
                    direction={sortDir}
                    onSort={handleSort}
                  />
                </TableHead>
              )}

              {col('capabilities') && <TableHead>Capabilities</TableHead>}
              {col('dialects') && <TableHead>Dialects</TableHead>}
              {col('inputModalities') && <TableHead>Input</TableHead>}
              {col('outputModalities') && <TableHead>Output</TableHead>}
            </TableRow>
          </TableHeader>

          <TableBody>
            {filteredRows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={ALL_COLUMNS.length}>
                  <div className="py-12 text-center">
                    <Text size="supporting" color="muted">
                      No models match your filters.
                    </Text>
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              filteredRows.map((row) => {
                const isFav = favourites.has(row.id);
                const isHovered = hoveredRow === row.id;
                return (
                  <TableRow
                    key={row.id}
                    onClick={() => setSelectedModel(row)}
                    onMouseEnter={() => setHoveredRow(row.id)}
                    onMouseLeave={() => setHoveredRow(null)}
                  >
                    {col('favourite') && (
                      <TableCell>
                        <div className="w-8 px-2 py-0 text-center">
                          {(isFav || isHovered) && (
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
                                  isFav ? 'fill-rose-500 text-rose-500' : 'text-muted-foreground',
                                ].join(' ')}
                              />
                            </button>
                          )}
                        </div>
                      </TableCell>
                    )}
                    {col('status') && (
                      <TableCell>
                        <div className="flex justify-center">
                          <AvailabilityDot status={row.status} />
                        </div>
                      </TableCell>
                    )}
                    {col('provider') && (
                      <TableCell>
                        <Text size="supporting">{row.owned_by}</Text>
                      </TableCell>
                    )}
                    {col('name') && (
                      <TableCell>
                        <div>
                          <Text size="supporting" weight="emphasis">
                            {row.name}
                          </Text>
                          <Text size="caption" color="muted" mono truncate>
                            {row.id}
                          </Text>
                        </div>
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
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </TableRoot>

      <ModelDetailModal model={selectedModel} onClose={() => setSelectedModel(null)} />
    </>
  );
}
