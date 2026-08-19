'use client';

import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { Badge, Button, Callout, Icon, Stack, Text } from '@orbit/terra';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { useLogsContext } from '../_context/logs-context';
import type { InferenceUsageRecord } from '../_types';
import { fmtCost, fmtDate, fmtTime, fmtNum, isExpired } from '@orbit/utils';
import {
  getInferenceStatusVariant,
  fmtInferenceStatus,
  fmtPrincipalType,
  resolvePrincipalDisplay,
} from '../_utils';
import { InferenceLogDetailModal } from './inference-log-detail-modal';
import { LogsToolbar } from './logs-toolbar';

// ── Status filter options ─────────────────────────────────────────────────────

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: 'success', label: 'Success' },
  { value: 'provider_error', label: 'Provider error' },
  { value: 'policy_denied', label: 'Policy denied' },
  { value: 'auth_error', label: 'Auth error' },
  { value: 'timeout', label: 'Timeout' },
  { value: 'routing_failed', label: 'Routing failed' },
  { value: 'canceled', label: 'Canceled' },
];

// ── LogsTab ───────────────────────────────────────────────────────────────────

export interface LogsTabProps {
  /** Pre-selected PAT UUID from the URL `?key=` param; passed by the shell. */
  selectedKeyId?: string;
}

export function LogsTab({ selectedKeyId = '' }: LogsTabProps) {
  const {
    variant,
    records,
    hasNextPage,
    hasPrevPage,
    pats,
    identities,
    principalFilterId,
    statusFilter,
    modelFilter,
    timeRange,
    error,
    status,
    refresh,
    setPrincipalFilter,
    setStatusFilter,
    setModelFilter,
    setTimeRange,
    goNext,
    goPrev,
  } = useLogsContext();

  const isAdminView = variant === 'all';

  const searchParams = useSearchParams();

  // Local state so the key filter's `selected` set stays in sync after changes.
  // `replaceState` doesn't trigger Next.js router updates, so we can't derive
  // this from searchParams; we own it here.
  const [activeKeyId, setActiveKeyId] = useState(selectedKeyId);
  const [selectedRecord, setSelectedRecord] = useState<InferenceUsageRecord | null>(null);

  // Key filter changes update the URL for shareability (deep-links) and call
  // refresh() to re-fetch client-side via the BFF — no RSC navigation, no
  // loading skeleton flash on every dropdown selection.
  const handleKeyChange = useCallback(
    (keyId: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (keyId) {
        params.set('key', keyId);
      } else {
        params.delete('key');
      }
      window.history.replaceState(null, '', `?${params.toString()}`);
      setActiveKeyId(keyId);
      refresh(keyId || undefined);
    },
    [searchParams, refresh],
  );

  // PAT ID → name lookup for the Key column cell.
  const patNameMap = useMemo(() => {
    const m = new Map<string, string>();
    pats.forEach((p) => m.set(p.id, p.name));
    return m;
  }, [pats]);

  // PAT ID → full PAT record lookup for the Key status column cell.
  const patMap = useMemo(() => new Map(pats.map((p) => [p.id, p])), [pats]);

  // Distinct models present in the loaded record set, filtered server-side by
  // raw model_id. Label prefers the virtual model ID (consumer-facing) so the
  // option reads the same as the Model column. Like principalOptions below, this
  // surfaces models from the loaded page(s) only — an accepted v1 limit until a
  // tenant-wide model roster is wired in.
  const modelOptions = useMemo(() => {
    const seen = new Map<string, string>();
    for (const r of records) {
      if (seen.has(r.model_id)) continue;
      seen.set(r.model_id, r.virtual_model_id ?? r.model_id);
    }
    return Array.from(seen.entries()).map(([value, label]) => ({ value, label }));
  }, [records]);

  const keyOptions = useMemo(
    () =>
      pats.map((p) => ({
        value: p.id,
        label: p.revoked_at ? `${p.name} (Revoked)` : p.name,
      })),
    [pats],
  );

  // Distinct principals present in the current record set, labelled via the
  // shared resolver. Admin 'all' view only. This surfaces principals from the
  // loaded page(s) rather than the full tenant roster — an accepted v1 limit:
  // agents and PATs have no tenant-wide directory to enumerate from.
  const principalOptions = useMemo(() => {
    if (!isAdminView) return [];
    const seen = new Map<string, string>();
    for (const r of records) {
      if (seen.has(r.principal_id)) continue;
      const disp = resolvePrincipalDisplay(
        r.principal_id,
        r.principal_type,
        identities[r.principal_id],
      );
      seen.set(r.principal_id, `${disp.primary} · ${fmtPrincipalType(r.principal_type)}`);
    }
    return Array.from(seen.entries()).map(([value, label]) => ({ value, label }));
  }, [isAdminView, records, identities]);

  // principal_id → principal_type, so toggling a principal filter can pass the
  // matching type through to the narrowing query.
  const principalTypeMap = useMemo(() => {
    const m = new Map<string, string>();
    for (const r of records) m.set(r.principal_id, r.principal_type);
    return m;
  }, [records]);

  // ── Principal column (admin 'all' view only) ────────────────────────────────
  const principalColumn: DataTableColumn<InferenceUsageRecord> = {
    id: 'principal',
    header: 'Principal',
    cell: (row) => {
      const disp = resolvePrincipalDisplay(
        row.principal_id,
        row.principal_type,
        identities[row.principal_id],
      );
      return (
        <Stack gap="none">
          <Text size="supporting" color="foreground">
            {disp.primary}
          </Text>
          <Text size="caption" color="subtle">
            {fmtPrincipalType(row.principal_type)}
          </Text>
        </Stack>
      );
    },
  };

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

    // Model — shows the virtual model ID (consumer-facing) with provider as subtitle.
    // Sortable alphabetically by virtual model ID.
    {
      id: 'model',
      header: 'Model',
      sortable: true,
      sortAccessor: (row) => row.virtual_model_id ?? row.model_id,
      cell: (row) => (
        <Stack gap="none">
          <Text size="supporting" color="foreground" mono>
            {row.virtual_model_id ?? row.model_id}
          </Text>
          {row.provider && (
            <Text size="caption" color="subtle" mono>
              {row.provider}
            </Text>
          )}
        </Stack>
      ),
    },

    // Provider model — raw provider-side model ID; hidden by default.
    {
      id: 'providerModel',
      header: 'Provider model',
      defaultVisible: false,
      sortable: true,
      sortAccessor: (row) => row.model_id,
      cell: (row) => (
        <Text size="supporting" color="muted" mono>
          {row.model_id}
        </Text>
      ),
    },

    // Status — filtered server-side via the toolbar (spans all pages).
    {
      id: 'status',
      header: 'Status',
      cell: (row) => (
        <Stack gap="tight" align="start">
          <Badge variant={getInferenceStatusVariant(row.status)} size="sm">
            {fmtInferenceStatus(row.status)}
          </Badge>
          {row.error_code && (
            <Text size="caption" color="muted" mono>
              {row.error_code}
            </Text>
          )}
        </Stack>
      ),
    },

    // Key — server-side filter: selecting a key calls refresh() for a
    // client-side BFF fetch; the URL is updated via replaceState for shareability.
    {
      id: 'key',
      header: 'Key',
      filter: {
        mode: 'server',
        options: keyOptions,
        selected: new Set(activeKeyId ? [activeKeyId] : []),
        // Single-select radio behaviour: clicking the active key deselects it.
        onToggle: (val) => handleKeyChange(val === activeKeyId ? '' : val),
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
        const pat = row.token_id ? patMap.get(row.token_id) : undefined;
        const keyStatus: 'active' | 'revoked' | 'expired' | 'unknown' | null = row.token_id
          ? pat
            ? pat.revoked_at
              ? 'revoked'
              : isExpired(pat.expires_at)
                ? 'expired'
                : 'active'
            : 'unknown'
          : null;

        if (keyStatus === 'unknown')
          return (
            <Badge variant="secondary" size="sm">
              Unknown
            </Badge>
          );
        if (keyStatus === 'revoked')
          return (
            <Badge variant="secondary" size="sm">
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

    // Tokens — right-aligned, sortable by effective total (including cache).
    {
      id: 'tokens',
      header: 'Tokens',
      align: 'right',
      sortable: true,
      sortAccessor: (row) => row.total_tokens,
      cell: (row) => (
        <Stack gap="none" align="end">
          <Text size="supporting" color="muted" mono align="end">
            {fmtNum(row.prompt_tokens)} in
          </Text>
          <Text size="supporting" color="muted" mono align="end">
            {fmtNum(row.completion_tokens)} out
          </Text>
          {row.cache_read_input_tokens > 0 && (
            <Text size="caption" color="subtle" mono align="end">
              +{fmtNum(row.cache_read_input_tokens)} cached
            </Text>
          )}
          {row.cache_creation_input_tokens > 0 && (
            <Text size="caption" color="subtle" mono align="end">
              +{fmtNum(row.cache_creation_input_tokens)} written
            </Text>
          )}
        </Stack>
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

  // Admin 'all' view surfaces the Principal column right after the Request ID,
  // so cross-principal rows are attributable at a glance. The self-scoped 'me'
  // view omits it — every row belongs to the caller. Rebuilt each render, like
  // `columns` above.
  const displayColumns = isAdminView ? [columns[0], principalColumn, ...columns.slice(1)] : columns;

  const deletedKeySelected = !!activeKeyId && status !== 'loading' && !patMap.has(activeKeyId);

  return (
    <Stack gap="comfortable">
      {error && (
        <Text size="supporting" color="status-error">
          {error}
        </Text>
      )}

      {deletedKeySelected && (
        <Callout variant="warning">
          <Text size="caption">
            The selected API key was not found. It may have been deleted or expired. Logs are still
            filtered to that key. Clear the key filter to see all logs.
          </Text>
        </Callout>
      )}

      <SmartTable
        tableId={isAdminView ? 'inferenceLogsAll' : 'inferenceLogs'}
        data={records}
        columns={displayColumns}
        getRowKey={(row) => row.id}
        noun="requests"
        searchPlaceholder="Search request ID or model…"
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) ||
          row.model_id.toLowerCase().includes(q) ||
          (row.virtual_model_id?.toLowerCase().includes(q) ?? false) ||
          (row.error_code?.toLowerCase().includes(q) ?? false) ||
          (isAdminView &&
            (row.principal_id.toLowerCase().includes(q) ||
              resolvePrincipalDisplay(
                row.principal_id,
                row.principal_type,
                identities[row.principal_id],
              )
                .primary.toLowerCase()
                .includes(q)))
        }
        defaultSortId="date"
        defaultSortDir="desc"
        emptyMessage="No inference requests yet."
        filteredEmptyMessage="No logs match your filters."
        onRowClick={setSelectedRecord}
        quickFilters={
          <LogsToolbar
            disabled={status === 'loading'}
            statusValue={statusFilter}
            statusOptions={STATUS_OPTIONS}
            onStatusChange={setStatusFilter}
            showPrincipal={isAdminView}
            principalValue={principalFilterId}
            principalOptions={principalOptions}
            onPrincipalChange={(val) =>
              setPrincipalFilter(val, val ? (principalTypeMap.get(val) ?? null) : null)
            }
            modelValue={modelFilter}
            modelOptions={modelOptions}
            onModelChange={setModelFilter}
            timeRange={timeRange}
            onTimeRangeChange={setTimeRange}
          />
        }
        toolbarActions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => refresh(activeKeyId || undefined)}
            disabled={status === 'loading'}
          >
            <Icon name="refresh-cw" size="sm" />
            {status === 'loading' ? 'Refreshing…' : 'Refresh'}
          </Button>
        }
      />

      {(hasPrevPage || hasNextPage) && (
        <Stack direction="horizontal" gap="tight" justify="end">
          <Button
            variant="outline"
            size="sm"
            onClick={goPrev}
            disabled={!hasPrevPage || status === 'loading'}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={goNext}
            disabled={!hasNextPage || status === 'loading'}
          >
            Next
          </Button>
        </Stack>
      )}

      <InferenceLogDetailModal
        record={selectedRecord}
        keyName={selectedRecord?.token_id ? patNameMap.get(selectedRecord.token_id) : undefined}
        onClose={() => setSelectedRecord(null)}
      />
    </Stack>
  );
}
