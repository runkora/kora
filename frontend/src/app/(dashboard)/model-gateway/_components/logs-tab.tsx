'use client';

import { useCallback, useMemo } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { Badge, DataTable, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { useLogsContext } from '../_context/logs-context';
import type { InferenceUsageRecord, InferenceUsageStatus } from '../_types';

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

// ── Status filter options ─────────────────────────────────────────────────────

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: 'success', label: 'Success' },
  { value: 'provider_error', label: 'Provider error' },
  { value: 'policy_denied', label: 'Policy denied' },
  { value: 'auth_error', label: 'Auth error' },
  { value: 'timeout', label: 'Timeout' },
  { value: 'routing_failed', label: 'Routing failed' },
];

// ── LogsTab ───────────────────────────────────────────────────────────────────

export interface LogsTabProps {
  /** Pre-selected PAT UUID from the URL `?key=` param; passed by the shell. */
  selectedKeyId?: string;
}

export function LogsTab({ selectedKeyId = '' }: LogsTabProps) {
  const { records, total, pats, error } = useLogsContext();

  const router = useRouter();
  const searchParams = useSearchParams();

  // Key filter changes re-navigate the RSC so a fresh server-side fetch is
  // issued with the correct token_id query parameter.
  const handleKeyChange = useCallback(
    (keyId: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (keyId) {
        params.set('key', keyId);
      } else {
        params.delete('key');
      }
      router.push(`?${params.toString()}`);
    },
    [searchParams, router],
  );

  // PAT ID → name lookup for the Key column cell.
  const patNameMap = useMemo(() => {
    const m = new Map<string, string>();
    pats.forEach((p) => m.set(p.id, p.name));
    return m;
  }, [pats]);

  const keyOptions = useMemo(() => pats.map((p) => ({ value: p.id, label: p.name })), [pats]);

  const columns: DataTableColumn<InferenceUsageRecord>[] = [
    // Request ID — always visible, locked.
    {
      id: 'requestId',
      header: 'Request ID',
      locked: true,
      cell: (row) => (
        <Text size="supporting" color="accent" mono>
          {row.id}
        </Text>
      ),
    },

    // Date — sortable by timestamp; displays date + time stacked.
    {
      id: 'date',
      header: 'Date',
      sortable: true,
      sortAccessor: (row) => new Date(row.created_at).getTime(),
      cell: (row) => (
        <Stack gap="none">
          <Text size="supporting" color="muted" mono>
            {fmtDate(row.created_at)}
          </Text>
          <Text size="caption" color="subtle" mono>
            {fmtTime(row.created_at)}
          </Text>
        </Stack>
      ),
    },

    // Model — sortable alphabetically.
    {
      id: 'model',
      header: 'Model',
      sortable: true,
      sortAccessor: (row) => row.model_id,
      cell: (row) => (
        <Text size="supporting" color="foreground" mono>
          {row.model_id}
        </Text>
      ),
    },

    // Status — client-side filter (no navigation required).
    {
      id: 'status',
      header: 'Status',
      filter: {
        mode: 'client',
        options: STATUS_OPTIONS,
        filterFn: (row, selected) => selected.has(row.status),
      },
      cell: (row) => (
        <Badge variant={getStatusVariant(row.status)} size="sm">
          {fmtStatus(row.status)}
        </Badge>
      ),
    },

    // Key — server-side filter: selecting a key triggers a router navigation
    // so the RSC re-fetches with the new token_id query parameter.
    {
      id: 'key',
      header: 'Key',
      filter: {
        mode: 'server',
        options: keyOptions,
        selected: new Set(selectedKeyId ? [selectedKeyId] : []),
        // Single-select radio behaviour: clicking the active key deselects it.
        onToggle: (val) => handleKeyChange(val === selectedKeyId ? '' : val),
        onClear: () => handleKeyChange(''),
      },
      cell: (row) => {
        const keyName = row.token_id ? (patNameMap.get(row.token_id) ?? 'Unknown key') : '—';
        const keyId = row.token_id ?? null;
        return (
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
        );
      },
    },

    // Key status — derived from the PAT list; not sortable or filterable.
    {
      id: 'keyStatus',
      header: 'Key status',
      cell: (row) => {
        const pat = row.token_id ? pats.find((p) => p.id === row.token_id) : undefined;
        const keyStatus: 'active' | 'expired' | 'revoked' | null = row.token_id
          ? pat
            ? isExpired(pat.expires_at)
              ? 'expired'
              : 'active'
            : 'revoked'
          : null;

        if (keyStatus === 'revoked')
          return (
            <Badge variant="error" size="sm">
              Revoked
            </Badge>
          );
        if (keyStatus === 'expired')
          return (
            <Badge variant="error" size="sm">
              Expired
            </Badge>
          );
        if (keyStatus === 'active')
          return (
            <Badge variant="healthy" size="sm" dot>
              Active
            </Badge>
          );
        return (
          <Text size="caption" color="subtle">
            —
          </Text>
        );
      },
    },

    // Latency — right-aligned, sortable.
    {
      id: 'latency',
      header: 'Latency',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.latency_ms,
      cell: (row) => (
        <Text size="supporting" color="muted" mono align="end">
          {row.latency_ms}ms
        </Text>
      ),
    },

    // Tokens — right-aligned, sortable by total.
    {
      id: 'tokens',
      header: 'Tokens in / out',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.total_tokens,
      cell: (row) => (
        <Text size="supporting" color="muted" mono align="end">
          {fmtNum(row.prompt_tokens)} / {fmtNum(row.completion_tokens)}
        </Text>
      ),
    },

    // Cost — right-aligned, sortable.
    {
      id: 'cost',
      header: 'Cost',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.estimated_cost_usd ?? 0,
      cell: (row) => (
        <Text size="supporting" color="foreground" mono align="end">
          {fmtCost(row.estimated_cost_usd)}
        </Text>
      ),
    },
  ];

  return (
    <Stack gap="comfortable">
      {error && (
        <Text size="supporting" color="status-error">
          {error}
        </Text>
      )}

      <DataTable
        data={records}
        columns={columns}
        getRowKey={(row) => row.id}
        totalCount={total}
        noun="requests"
        searchPlaceholder="Search request ID or model…"
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) || row.model_id.toLowerCase().includes(q)
        }
        defaultSortId="date"
        defaultSortDir="desc"
        emptyMessage="No inference requests yet."
        filteredEmptyMessage="No logs match your filters."
      />
    </Stack>
  );
}
