'use client';

import { useState, useMemo, useCallback } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Check,
  ListFilter,
  Search,
  Settings2,
} from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Popover,
  PopoverContent,
  PopoverTrigger,
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
import { useLogsContext } from '../_context/logs-context';
import type { InferenceUsageRecord, InferenceUsageStatus } from '../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

type SortDir = 'asc' | 'desc' | null;
type SortKey = 'date' | 'model' | 'latency' | 'tokens' | 'cost' | null;
type LogColumnId =
  | 'date'
  | 'model'
  | 'status'
  | 'key'
  | 'keyStatus'
  | 'latency'
  | 'tokens'
  | 'cost';

// ── Constants ─────────────────────────────────────────────────────────────────

interface LogColumnDef {
  id: LogColumnId;
  label: string;
}

const LOG_COLUMNS: LogColumnDef[] = [
  { id: 'date', label: 'Date' },
  { id: 'model', label: 'Model' },
  { id: 'status', label: 'Status' },
  { id: 'key', label: 'Key' },
  { id: 'keyStatus', label: 'Key status' },
  { id: 'latency', label: 'Latency' },
  { id: 'tokens', label: 'Tokens' },
  { id: 'cost', label: 'Cost' },
];

const DEFAULT_VISIBLE: Set<LogColumnId> = new Set([
  'date',
  'model',
  'status',
  'key',
  'keyStatus',
  'latency',
  'tokens',
  'cost',
]);

const STATUS_OPTIONS: { value: InferenceUsageStatus; label: string }[] = [
  { value: 'success', label: 'Success' },
  { value: 'provider_error', label: 'Provider error' },
  { value: 'policy_denied', label: 'Policy denied' },
  { value: 'auth_error', label: 'Auth error' },
  { value: 'timeout', label: 'Timeout' },
  { value: 'routing_failed', label: 'Routing failed' },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

function getStatusVariant(status: InferenceUsageStatus) {
  switch (status) {
    case 'success':
      return 'healthy' as const;
    case 'policy_denied':
    case 'auth_error':
      return 'degraded' as const;
    default:
      return 'error' as const;
  }
}

function fmtStatus(status: InferenceUsageStatus): string {
  return status.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

function fmtCost(usd: number | null): string {
  if (usd === null || usd === undefined) return '—';
  if (usd === 0) return '$0.00';
  if (usd < 0.0001) return '<$0.0001';
  return `$${usd.toFixed(4)}`;
}

function fmtDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function fmtNum(n: number): string {
  return n.toLocaleString();
}

function isExpired(expiresAt: string | null | undefined): boolean {
  if (!expiresAt) return false;
  return new Date(expiresAt) < new Date();
}

function sortRecords(
  records: InferenceUsageRecord[],
  key: SortKey,
  dir: SortDir,
): InferenceUsageRecord[] {
  if (!key || !dir) return records;
  return [...records].sort((a, b) => {
    let av = 0;
    let bv = 0;
    if (key === 'date') {
      av = new Date(a.CreatedAt).getTime();
      bv = new Date(b.CreatedAt).getTime();
    } else if (key === 'latency') {
      av = a.LatencyMS;
      bv = b.LatencyMS;
    } else if (key === 'cost') {
      av = a.EstimatedCostUSD ?? 0;
      bv = b.EstimatedCostUSD ?? 0;
    } else if (key === 'tokens') {
      av = a.TotalTokens;
      bv = b.TotalTokens;
    } else if (key === 'model') {
      return dir === 'asc'
        ? a.ModelID.localeCompare(b.ModelID)
        : b.ModelID.localeCompare(a.ModelID);
    }
    return dir === 'asc' ? av - bv : bv - av;
  });
}

// ── Sub-components ────────────────────────────────────────────────────────────

function SearchInput({
  query,
  onQueryChange,
}: {
  query: string;
  onQueryChange: (q: string) => void;
}) {
  return (
    <label className="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-sm max-w-xs w-full focus-within:ring-2 focus-within:ring-ring">
      <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <input
        type="search"
        aria-label="Search request ID or model"
        placeholder="Search request ID or model…"
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        className="flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
      />
    </label>
  );
}

function SortButton({
  label,
  columnKey,
  currentKey,
  direction,
  onSort,
}: {
  label: string;
  columnKey: NonNullable<SortKey>;
  currentKey: SortKey;
  direction: SortDir;
  onSort: (key: NonNullable<SortKey>) => void;
}) {
  const active = currentKey === columnKey;
  const Icon =
    active && direction === 'asc'
      ? ArrowUp
      : active && direction === 'desc'
        ? ArrowDown
        : ArrowUpDown;
  return (
    <button
      onClick={() => onSort(columnKey)}
      className="flex items-center gap-1 text-inherit hover:text-foreground transition-colors"
    >
      {label}
      <Icon className="h-3 w-3 opacity-60" />
    </button>
  );
}

function FilterPopover({
  options,
  selected,
  onToggle,
  onClear,
}: {
  options: { value: string; label: string }[];
  selected: Set<string>;
  onToggle: (value: string) => void;
  onClear: () => void;
}) {
  const hasSelection = selected.size > 0;
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Filter"
          className={[
            'relative inline-flex items-center justify-center rounded p-0.5 transition-colors',
            'hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
            hasSelection ? 'text-accent' : 'text-muted-foreground hover:text-foreground',
          ].join(' ')}
        >
          <ListFilter className="size-3" aria-hidden />
          {hasSelection && (
            <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-accent" />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start">
        <Stack asChild gap="none">
          <menu className="w-48 p-1 list-none m-0">
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
                {opt.label}
              </button>
            ))}
            {hasSelection && (
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

function LogColumnToggle({
  visible,
  onToggle,
}: {
  visible: Set<LogColumnId>;
  onToggle: (id: LogColumnId) => void;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" aria-label="Toggle column visibility">
          <Settings2 className="size-3.5" />
          Columns
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <Stack asChild gap="none">
          <menu className="w-44 p-0 list-none m-0">
            <li className="border-b border-border px-3 py-2">
              <Text size="caption" color="muted">
                Toggle columns
              </Text>
            </li>
            <fieldset className="py-1 border-0 m-0 p-0">
              <legend className="sr-only">Visible columns</legend>
              {LOG_COLUMNS.map((col) => (
                <label
                  key={col.id}
                  className="flex cursor-pointer items-center gap-2.5 px-3 py-2 text-sm transition-colors hover:bg-surface-elevated"
                >
                  <input
                    type="checkbox"
                    checked={visible.has(col.id)}
                    onChange={() => onToggle(col.id)}
                    className="accent-accent size-3.5 cursor-pointer rounded"
                  />
                  <Text size="supporting">{col.label}</Text>
                </label>
              ))}
            </fieldset>
          </menu>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

// ── Logs Tab ──────────────────────────────────────────────────────────────────

export interface LogsTabProps {
  /** Pre-selected PAT UUID from the URL `?key=` param; passed by the shell. */
  selectedKeyId?: string;
}

export function LogsTab({ selectedKeyId = '' }: LogsTabProps) {
  const { records, total, pats, error } = useLogsContext();

  const router = useRouter();
  const searchParams = useSearchParams();

  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilters, setStatusFilters] = useState<Set<string>>(new Set());
  const [sortKey, setSortKey] = useState<SortKey>('date');
  const [sortDir, setSortDir] = useState<SortDir>('desc');
  const [visible, setVisible] = useState<Set<LogColumnId>>(DEFAULT_VISIBLE);

  // Build a PAT ID → name lookup for the key column.
  const patNameMap = useMemo(() => {
    const m = new Map<string, string>();
    pats.forEach((p) => m.set(p.id, p.name));
    return m;
  }, [pats]);

  const handleSort = useCallback((key: NonNullable<SortKey>) => {
    setSortKey((prev) => {
      if (prev === key) {
        setSortDir((d) => (d === 'asc' ? 'desc' : d === 'desc' ? null : 'asc'));
        return prev;
      }
      setSortDir('asc');
      return key;
    });
  }, []);

  const toggleColumn = useCallback((id: LogColumnId) => {
    setVisible((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleStatus = useCallback((s: string) => {
    setStatusFilters((prev) => {
      const next = new Set(prev);
      if (next.has(s)) next.delete(s);
      else next.add(s);
      return next;
    });
  }, []);

  // Client-side filtering of the server-fetched page.
  const filteredRecords = useMemo(() => {
    let result = records;

    if (statusFilters.size > 0) {
      result = result.filter((r) => statusFilters.has(r.Status));
    }

    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      result = result.filter(
        (r) => r.ID.toLowerCase().includes(q) || r.ModelID.toLowerCase().includes(q),
      );
    }

    return sortRecords(result, sortKey, sortDir);
  }, [records, searchQuery, statusFilters, sortKey, sortDir]);

  // Key filter changes re-navigate the RSC so a fresh server-side fetch is
  // issued with the correct token_id query parameter.
  function handleKeyChange(keyId: string) {
    const params = new URLSearchParams(searchParams.toString());
    if (keyId) {
      params.set('key', keyId);
    } else {
      params.delete('key');
    }
    router.push(`?${params.toString()}`);
  }

  function col(id: LogColumnId): boolean {
    return visible.has(id);
  }

  // 1 (Request ID, always on) + each visible optional column.
  const colSpan = 1 + LOG_COLUMNS.filter((c) => visible.has(c.id)).length;

  // Build key filter options for the Key column header popover.
  const keyOptions = useMemo(() => pats.map((p) => ({ value: p.id, label: p.name })), [pats]);
  const selectedKeySet = useMemo(
    () => (selectedKeyId ? new Set([selectedKeyId]) : new Set<string>()),
    [selectedKeyId],
  );

  return (
    <Stack gap="comfortable">
      {/* Error */}
      {error && (
        <Text size="supporting" color="status-error">
          {error}
        </Text>
      )}

      {/* Toolbar: search | spacer | count | columns */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <SearchInput query={searchQuery} onQueryChange={setSearchQuery} />
        <span className="flex-1" />
        <Text size="caption" color="muted">
          {filteredRecords.length} of {total} requests
        </Text>
        <LogColumnToggle visible={visible} onToggle={toggleColumn} />
      </Stack>

      {/* Logs Table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  {/* Request ID — always visible */}
                  <TableHead>Request ID</TableHead>

                  {col('date') && (
                    <TableHead>
                      <SortButton
                        label="Date"
                        columnKey="date"
                        currentKey={sortKey}
                        direction={sortDir}
                        onSort={handleSort}
                      />
                    </TableHead>
                  )}

                  {col('model') && (
                    <TableHead>
                      <SortButton
                        label="Model"
                        columnKey="model"
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
                          selected={statusFilters}
                          onToggle={toggleStatus}
                          onClear={() => setStatusFilters(new Set())}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('key') && (
                    <TableHead>
                      <Stack direction="horizontal" align="center" gap="tight">
                        <span>Key</span>
                        <FilterPopover
                          options={keyOptions}
                          selected={selectedKeySet}
                          onToggle={(keyId) =>
                            handleKeyChange(keyId === selectedKeyId ? '' : keyId)
                          }
                          onClear={() => handleKeyChange('')}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('keyStatus') && <TableHead>Key status</TableHead>}

                  {col('latency') && (
                    <TableHead>
                      <Stack direction="horizontal" justify="end">
                        <SortButton
                          label="Latency"
                          columnKey="latency"
                          currentKey={sortKey}
                          direction={sortDir}
                          onSort={handleSort}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('tokens') && (
                    <TableHead>
                      <Stack direction="horizontal" justify="end">
                        <SortButton
                          label="Tokens in / out"
                          columnKey="tokens"
                          currentKey={sortKey}
                          direction={sortDir}
                          onSort={handleSort}
                        />
                      </Stack>
                    </TableHead>
                  )}

                  {col('cost') && (
                    <TableHead>
                      <Stack direction="horizontal" justify="end">
                        <SortButton
                          label="Cost"
                          columnKey="cost"
                          currentKey={sortKey}
                          direction={sortDir}
                          onSort={handleSort}
                        />
                      </Stack>
                    </TableHead>
                  )}
                </TableRow>
              </TableHeader>

              <TableBody>
                {filteredRecords.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={colSpan}>
                      <Stack align="center" justify="center">
                        <Stack asChild>
                          <figure className="py-8 text-center m-0">
                            <Text size="supporting" color="muted">
                              {searchQuery || statusFilters.size > 0
                                ? 'No logs match your filters'
                                : 'No inference requests yet'}
                            </Text>
                          </figure>
                        </Stack>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredRecords.map((record) => {
                    const statusVariant = getStatusVariant(record.Status);
                    const keyName = record.TokenID
                      ? (patNameMap.get(record.TokenID) ?? 'Unknown key')
                      : '—';
                    const keyId = record.TokenID ?? null;

                    // Determine key status by presence in the active PAT list.
                    // Revoked PATs are removed from the list endpoint entirely, so a
                    // TokenID that is set but not found in the list implies revocation.
                    // Expired PATs remain in the list but have an expires_at in the past.
                    const pat = record.TokenID
                      ? pats.find((p) => p.id === record.TokenID)
                      : undefined;
                    const keyStatus: 'active' | 'expired' | 'revoked' | null = record.TokenID
                      ? pat
                        ? isExpired(pat.expires_at)
                          ? 'expired'
                          : 'active'
                        : 'revoked'
                      : null;

                    return (
                      <TableRow key={record.ID}>
                        {/* Request ID — always visible */}
                        <TableCell>
                          <Text size="supporting" color="accent" mono>
                            {record.ID}
                          </Text>
                        </TableCell>

                        {/* Date + Time stacked */}
                        {col('date') && (
                          <TableCell>
                            <Stack gap="none">
                              <Text size="supporting" color="muted" mono title={record.CreatedAt}>
                                {fmtDate(record.CreatedAt)}
                              </Text>
                              <Text size="caption" color="subtle" mono>
                                {fmtTime(record.CreatedAt)}
                              </Text>
                            </Stack>
                          </TableCell>
                        )}

                        {/* Model */}
                        {col('model') && (
                          <TableCell>
                            <Text size="supporting" color="foreground" mono>
                              {record.ModelID}
                            </Text>
                          </TableCell>
                        )}

                        {/* Inference Status */}
                        {col('status') && (
                          <TableCell>
                            <Badge variant={statusVariant} size="sm">
                              {fmtStatus(record.Status)}
                            </Badge>
                          </TableCell>
                        )}

                        {/* Key name + UUID */}
                        {col('key') && (
                          <TableCell>
                            <Stack gap="none">
                              <Text size="supporting" color="foreground">
                                {keyName}
                              </Text>
                              {keyId && (
                                <Text size="caption" color="subtle" mono>
                                  {keyId}
                                </Text>
                              )}
                            </Stack>
                          </TableCell>
                        )}

                        {/* Key status */}
                        {col('keyStatus') && (
                          <TableCell>
                            {keyStatus === 'revoked' ? (
                              <Badge variant="error" size="sm">
                                Revoked
                              </Badge>
                            ) : keyStatus === 'expired' ? (
                              <Badge variant="error" size="sm">
                                Expired
                              </Badge>
                            ) : keyStatus === 'active' ? (
                              <Badge variant="healthy" size="sm" dot>
                                Active
                              </Badge>
                            ) : (
                              <Text size="caption" color="subtle">
                                —
                              </Text>
                            )}
                          </TableCell>
                        )}

                        {/* Latency */}
                        {col('latency') && (
                          <TableCell>
                            <Stack direction="horizontal" justify="end">
                              <Text size="supporting" color="muted" mono>
                                {record.LatencyMS}ms
                              </Text>
                            </Stack>
                          </TableCell>
                        )}

                        {/* Tokens In / Out */}
                        {col('tokens') && (
                          <TableCell>
                            <Stack direction="horizontal" justify="end">
                              <Text size="supporting" color="muted" mono>
                                {fmtNum(record.PromptTokens)} / {fmtNum(record.CompletionTokens)}
                              </Text>
                            </Stack>
                          </TableCell>
                        )}

                        {/* Cost */}
                        {col('cost') && (
                          <TableCell>
                            <Stack direction="horizontal" justify="end">
                              <Text size="supporting" color="foreground" mono>
                                {fmtCost(record.EstimatedCostUSD)}
                              </Text>
                            </Stack>
                          </TableCell>
                        )}
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>
    </Stack>
  );
}
