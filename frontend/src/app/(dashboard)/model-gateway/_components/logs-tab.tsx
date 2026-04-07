'use client';

import { useState, useMemo } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
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
import { Search, RefreshCw } from 'lucide-react';

// ── Types & Mock Data ─────────────────────────────────────────────────────────

interface LogEntry {
  id: string;
  time: string;
  model: string;
  status: number;
  latency: number;
  tokensIn: number;
  tokensOut: number;
  cost: number;
  keyId: string;
  keyName: string;
}

const MOCK_LOGS: LogEntry[] = [
  {
    id: 'req_a1',
    time: '09:42:18',
    model: 'claude-sonnet-4',
    status: 200,
    latency: 1240,
    tokensIn: 1842,
    tokensOut: 623,
    cost: 0.0089,
    keyId: 'key_01HZ9K3V',
    keyName: 'Production Agent Fleet',
  },
  {
    id: 'req_a2',
    time: '09:42:16',
    model: 'claude-haiku-3.5',
    status: 200,
    latency: 320,
    tokensIn: 524,
    tokensOut: 189,
    cost: 0.0004,
    keyId: 'key_01HZ9K3V',
    keyName: 'Production Agent Fleet',
  },
  {
    id: 'req_a3',
    time: '09:42:14',
    model: 'gpt-4o',
    status: 200,
    latency: 2100,
    tokensIn: 3200,
    tokensOut: 1420,
    cost: 0.021,
    keyId: 'key_02JA8M4W',
    keyName: 'Staging Environment',
  },
  {
    id: 'req_a4',
    time: '09:42:11',
    model: 'gemini-2.0-flash',
    status: 429,
    latency: 45,
    tokensIn: 0,
    tokensOut: 0,
    cost: 0,
    keyId: 'key_01HZ9K3V',
    keyName: 'Production Agent Fleet',
  },
  {
    id: 'req_a5',
    time: '09:42:09',
    model: 'claude-sonnet-4',
    status: 200,
    latency: 980,
    tokensIn: 980,
    tokensOut: 412,
    cost: 0.0062,
    keyId: 'key_04LC0P6Y',
    keyName: 'CI Pipeline Runner',
  },
  {
    id: 'req_a6',
    time: '09:42:06',
    model: 'claude-haiku-3.5',
    status: 200,
    latency: 210,
    tokensIn: 312,
    tokensOut: 98,
    cost: 0.0002,
    keyId: 'key_03KB9N5X',
    keyName: 'Dev / Local Testing',
  },
  {
    id: 'req_a7',
    time: '09:42:03',
    model: 'gpt-4o',
    status: 500,
    latency: 8200,
    tokensIn: 0,
    tokensOut: 0,
    cost: 0,
    keyId: 'key_01HZ9K3V',
    keyName: 'Production Agent Fleet',
  },
  {
    id: 'req_a8',
    time: '09:41:58',
    model: 'claude-opus-4',
    status: 200,
    latency: 4800,
    tokensIn: 6200,
    tokensOut: 2100,
    cost: 0.089,
    keyId: 'key_01HZ9K3V',
    keyName: 'Production Agent Fleet',
  },
];

// ── Types ─────────────────────────────────────────────────────────────────────

type StatusFilter = 'all' | '2xx' | '4xx/5xx';

// ── Helpers ───────────────────────────────────────────────────────────────────

function getStatusVariant(status: number): 'healthy' | 'degraded' | 'error' {
  if (status >= 200 && status < 300) return 'healthy';
  if (status >= 400 && status < 500) return 'degraded';
  return 'error';
}

function fmtCost(amount: number): string {
  return `$${amount.toFixed(4)}`;
}

function filterLogsByStatus(logs: LogEntry[], filter: StatusFilter): LogEntry[] {
  if (filter === 'all') return logs;
  if (filter === '2xx') return logs.filter((log) => log.status >= 200 && log.status < 300);
  return logs.filter((log) => (log.status >= 400 && log.status < 500) || log.status >= 500);
}

// ── Status Filter Button ──────────────────────────────────────────────────────

interface StatusFilterProps {
  activeFilter: StatusFilter;
  onFilterChange: (filter: StatusFilter) => void;
}

function StatusFilter({ activeFilter, onFilterChange }: StatusFilterProps) {
  const filters: StatusFilter[] = ['all', '2xx', '4xx/5xx'];

  return (
    <Stack direction="horizontal" gap="snug">
      {filters.map((filter) => (
        <Button
          key={filter}
          size="xs"
          variant={activeFilter === filter ? 'secondary' : 'ghost'}
          onClick={() => onFilterChange(filter)}
        >
          {filter === 'all' ? 'All' : filter}
        </Button>
      ))}
    </Stack>
  );
}

// ── Search Input ──────────────────────────────────────────────────────────────

interface SearchInputProps {
  query: string;
  onQueryChange: (query: string) => void;
}

function SearchInput({ query, onQueryChange }: SearchInputProps) {
  return (
    <span className="relative flex-1">
      <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted pointer-events-none" />
      <input
        type="text"
        placeholder="Search by model, key, or request ID..."
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        className="w-full rounded-md border border-border bg-surface px-3 py-1.5 pl-8 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />
    </span>
  );
}

// ── Logs Tab Component ────────────────────────────────────────────────────────

export function LogsTab() {
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [isLive, setIsLive] = useState(false);

  // Filter logs by status and search query
  const filteredLogs = useMemo(() => {
    let result = filterLogsByStatus(MOCK_LOGS, statusFilter);

    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase();
      result = result.filter(
        (log) =>
          log.id.toLowerCase().includes(query) ||
          log.model.toLowerCase().includes(query) ||
          log.keyId.toLowerCase().includes(query) ||
          log.keyName.toLowerCase().includes(query),
      );
    }

    return result;
  }, [searchQuery, statusFilter]);

  return (
    <Stack gap="comfortable">
      {/* Toolbar */}
      <Stack direction="horizontal" align="center" justify="between" gap="comfortable" wrap>
        <SearchInput query={searchQuery} onQueryChange={setSearchQuery} />
        <StatusFilter activeFilter={statusFilter} onFilterChange={setStatusFilter} />
        <span className="shrink-0">
          <Button
            variant={isLive ? 'secondary' : 'ghost'}
            size="sm"
            onClick={() => setIsLive(!isLive)}
          >
            <RefreshCw className="h-4 w-4 mr-1.5" />
            Live
          </Button>
        </span>
      </Stack>

      {/* Logs Table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Request ID</TableHead>
                  <TableHead>Time</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>
                    <Stack direction="horizontal" justify="end">
                      Latency
                    </Stack>
                  </TableHead>
                  <TableHead>
                    <Stack direction="horizontal" justify="end">
                      Tokens in / out
                    </Stack>
                  </TableHead>
                  <TableHead>
                    <Stack direction="horizontal" justify="end">
                      Cost
                    </Stack>
                  </TableHead>
                  <TableHead>Key</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredLogs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8}>
                      <Stack align="center" justify="center">
                        <Stack asChild>
                          <figure className="py-8 text-center m-0">
                            <Text size="supporting" color="muted">
                              {searchQuery || statusFilter !== 'all'
                                ? 'No logs match your filters'
                                : 'No logs available'}
                            </Text>
                          </figure>
                        </Stack>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredLogs.map((log) => {
                    const statusVariant = getStatusVariant(log.status);
                    return (
                      <TableRow key={log.id}>
                        {/* Request ID */}
                        <TableCell>
                          <Text size="supporting" color="accent" mono>
                            {log.id}
                          </Text>
                        </TableCell>

                        {/* Time */}
                        <TableCell>
                          <Text size="supporting" color="muted" mono>
                            {log.time}
                          </Text>
                        </TableCell>

                        {/* Model */}
                        <TableCell>
                          <Text size="supporting" color="foreground" mono>
                            {log.model}
                          </Text>
                        </TableCell>

                        {/* Status Badge */}
                        <TableCell>
                          <Badge variant={statusVariant} size="sm">
                            {log.status}
                          </Badge>
                        </TableCell>

                        {/* Latency */}
                        <TableCell>
                          <Stack direction="horizontal" justify="end">
                            <Text size="supporting" color="muted" mono>
                              {log.latency}ms
                            </Text>
                          </Stack>
                        </TableCell>

                        {/* Tokens In / Out */}
                        <TableCell>
                          <Stack direction="horizontal" justify="end">
                            <Text size="supporting" color="muted" mono>
                              {log.tokensIn} / {log.tokensOut}
                            </Text>
                          </Stack>
                        </TableCell>

                        {/* Cost */}
                        <TableCell>
                          <Stack direction="horizontal" justify="end">
                            <Text size="supporting" color="foreground" mono>
                              {fmtCost(log.cost)}
                            </Text>
                          </Stack>
                        </TableCell>

                        {/* Key */}
                        <TableCell>
                          <Stack gap="none">
                            <Text size="supporting" color="foreground">
                              {log.keyName}
                            </Text>
                            <Text size="caption" color="subtle" mono>
                              {log.keyId}
                            </Text>
                          </Stack>
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
    </Stack>
  );
}
