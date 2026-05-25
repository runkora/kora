'use client';

/** @tier 3 — agents/[slug]-scoped. Live log stream tab for the agent detail page. */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Card, CardContent, CardHeader, Icon, Input, Stack, Text } from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

type LogLevel = 'INFO' | 'TOOL' | 'APPR' | 'WARN' | 'ERR';
type FilterLevel = LogLevel | 'ALL';

interface LogLine {
  time: string;
  level: LogLevel;
  message: string;
}

// ── Mock data ─────────────────────────────────────────────────────────────────

const MOCK_LOGS: LogLine[] = [
  { time: '09:41:03', level: 'INFO', message: 'Agent initialised · session=a3f8c2d1' },
  {
    time: '09:41:03',
    level: 'INFO',
    message: 'Model loaded · anthropic/claude-sonnet-4-6 · max_tokens=8192',
  },
  { time: '09:41:03', level: 'INFO', message: 'System prompt injected · 1,247 tokens' },
  { time: '09:41:04', level: 'TOOL', message: 'read_file src/auth/login.tsx → 142 lines' },
  { time: '09:41:04', level: 'TOOL', message: 'read_file src/auth/types.ts → 28 lines' },
  { time: '09:41:05', level: 'TOOL', message: 'read_file src/auth/middleware.ts → 67 lines' },
  {
    time: '09:41:06',
    level: 'INFO',
    message: 'Planning phase complete · 3 files read · 237 lines analysed',
  },
  { time: '09:41:07', level: 'APPR', message: 'bash npx tsc --noEmit · awaiting approval…' },
  { time: '09:41:09', level: 'INFO', message: 'Approval granted · dev@orbit.local' },
  { time: '09:41:09', level: 'TOOL', message: 'bash npx tsc --noEmit → exit 1 · 3 errors' },
  {
    time: '09:41:09',
    level: 'WARN',
    message: "Type error: Property 'user' does not exist on type 'Session'",
  },
  { time: '09:41:10', level: 'TOOL', message: 'write_file src/auth/login.tsx → +12 / -3' },
  { time: '09:41:11', level: 'TOOL', message: 'write_file src/lib/api-client.ts → +4 / -1' },
  { time: '09:41:12', level: 'TOOL', message: 'bash npx tsc --noEmit → exit 0' },
  {
    time: '09:41:13',
    level: 'APPR',
    message: 'bash npm test src/auth -- --coverage · awaiting approval…',
  },
  { time: '09:41:15', level: 'INFO', message: 'Approval granted · dev@orbit.local' },
  {
    time: '09:41:15',
    level: 'TOOL',
    message: 'bash npm test src/auth -- --coverage → exit 0 · 14 passed',
  },
  { time: '09:41:16', level: 'INFO', message: 'All checks passed · preparing commit' },
  { time: '09:41:17', level: 'TOOL', message: 'bash git add -p → 2 files staged' },
  {
    time: '09:41:18',
    level: 'APPR',
    message: 'bash git commit -m "fix(auth): resolve Session type error" · awaiting approval…',
  },
  { time: '09:41:20', level: 'INFO', message: 'Approval granted · dev@orbit.local' },
  { time: '09:41:20', level: 'TOOL', message: 'bash git commit → abc123f' },
  {
    time: '09:41:21',
    level: 'INFO',
    message: 'Run completed · duration=78s · 8 tool calls · 3 approvals',
  },
];

// ── Level styling ─────────────────────────────────────────────────────────────

const LEVEL_CHIP: Record<LogLevel, { bg: string; color: string }> = {
  INFO: { bg: 'oklch(22% 0.04 230 / 0.5)', color: 'oklch(72% 0.10 230)' },
  TOOL: { bg: 'oklch(22% 0.08 290 / 0.5)', color: 'oklch(74% 0.12 290)' },
  APPR: { bg: 'oklch(22% 0.07  75 / 0.5)', color: 'oklch(78% 0.15  75)' },
  WARN: { bg: 'oklch(22% 0.10  70 / 0.5)', color: 'oklch(83% 0.18  70)' },
  ERR: { bg: 'oklch(22% 0.10  20 / 0.5)', color: 'oklch(65% 0.20  20)' },
};

const LEVEL_ROW_BG: Partial<Record<LogLevel, string>> = {
  APPR: 'oklch(var(--color-surface-elevated) / 0.5)',
  WARN: 'oklch(22% 0.07 70 / 0.10)',
  ERR: 'oklch(22% 0.07 20 / 0.10)',
};

const LEVEL_ORDER: LogLevel[] = ['INFO', 'TOOL', 'APPR', 'WARN', 'ERR'];

const FILTERS: { value: FilterLevel; label: string }[] = [
  { value: 'ALL', label: 'All' },
  { value: 'INFO', label: 'Info' },
  { value: 'TOOL', label: 'Tool' },
  { value: 'APPR', label: 'Appr' },
  { value: 'WARN', label: 'Warn' },
  { value: 'ERR', label: 'Error' },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function AgentLogsTab() {
  const scrollRef = useRef<HTMLDivElement>(null);

  const [filter, setFilter] = useState<FilterLevel>('ALL');
  const [search, setSearch] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const [copied, setCopied] = useState(false);

  // ── Filtering ───────────────────────────────────────────────────────────────
  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return MOCK_LOGS.filter((line) => {
      if (filter !== 'ALL' && line.level !== filter) return false;
      if (q && !line.message.toLowerCase().includes(q) && !line.level.toLowerCase().includes(q))
        return false;
      return true;
    });
  }, [filter, search]);

  // ── Stats (over full log, not filtered view) ────────────────────────────────
  const stats = useMemo(() => {
    const counts: Partial<Record<LogLevel, number>> = {};
    for (const line of MOCK_LOGS) {
      counts[line.level] = (counts[line.level] ?? 0) + 1;
    }
    return counts;
  }, []);

  // ── Auto-scroll ─────────────────────────────────────────────────────────────
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered, autoScroll]);

  // ── Copy ────────────────────────────────────────────────────────────────────
  const handleCopy = useCallback(() => {
    const text = filtered.map((l) => `[${l.time}] ${l.level.padEnd(4)}  ${l.message}`).join('\n');
    void navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [filtered]);

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <>
      <style>{`
        @keyframes log-pulse {
          0%, 100% { opacity: 1; }
          50%       { opacity: 0.25; }
        }
      `}</style>

      <Stack gap="snug" paddingTop="comfortable">
        <Card>
          {/* Header row: title + streaming indicator | auto-scroll + copy */}
          <CardHeader
            compact
            leadingIcon="scroll-text"
            title="Live Logs"
            action={
              <Stack direction="horizontal" align="center" gap="snug">
                <Stack direction="horizontal" align="center" gap="tight">
                  <Stack
                    style={{
                      width: '6px',
                      height: '6px',
                      borderRadius: '50%',
                      flexShrink: 0,
                      background: 'oklch(70% 0.12 140)',
                      animation: 'log-pulse 1.5s ease-in-out infinite',
                    }}
                  />
                  <Text size="caption" color="muted">
                    streaming
                  </Text>
                </Stack>
                <Button
                  variant={autoScroll ? 'default' : 'ghost'}
                  size="icon-sm"
                  title={autoScroll ? 'Auto-scroll on' : 'Auto-scroll off'}
                  aria-label="Toggle auto-scroll"
                  onClick={() => setAutoScroll((v) => !v)}
                >
                  <Icon name="arrow-down" size="2xs" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  title="Copy visible logs"
                  aria-label="Copy logs to clipboard"
                  onClick={handleCopy}
                >
                  <Icon name={copied ? 'check' : 'copy'} size="2xs" />
                </Button>
              </Stack>
            }
            divider
          >
            {/* Filter chips + search — rendered as children inside CardHeader */}
            <Stack
              direction="horizontal"
              align="center"
              gap="snug"
              style={{ paddingTop: '6px', paddingBottom: '2px' }}
            >
              <Stack direction="horizontal" gap="tight" style={{ flex: 1 }}>
                {FILTERS.map((f) => {
                  const active = filter === f.value;
                  const chip = f.value !== 'ALL' ? LEVEL_CHIP[f.value as LogLevel] : null;
                  const count = f.value !== 'ALL' ? (stats[f.value as LogLevel] ?? 0) : null;
                  return (
                    <Button
                      key={f.value}
                      size="sm"
                      variant={active ? 'default' : 'ghost'}
                      onClick={() => setFilter(f.value)}
                      style={
                        active && chip
                          ? {
                              background: chip.bg,
                              color: chip.color,
                              borderColor: `${chip.color}44`,
                            }
                          : undefined
                      }
                    >
                      {f.label}
                      {count !== null && count > 0 && (
                        <Text
                          mono
                          size="caption"
                          style={{
                            marginLeft: '3px',
                            opacity: 0.65,
                            color: active && chip ? chip.color : undefined,
                          }}
                        >
                          {count}
                        </Text>
                      )}
                    </Button>
                  );
                })}
              </Stack>
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search…"
                style={{ maxWidth: '160px' }}
              />
            </Stack>
          </CardHeader>

          {/* Stats strip */}
          <Stack
            direction="horizontal"
            align="center"
            gap="comfortable"
            style={{
              padding: '6px 16px',
              borderBottom: '1px solid oklch(var(--color-border) / 1)',
              background: 'oklch(var(--color-surface-elevated) / 0.4)',
            }}
          >
            <Text size="caption" color="subtle">
              {MOCK_LOGS.length} events
            </Text>
            {LEVEL_ORDER.filter((lvl) => (stats[lvl] ?? 0) > 0).map((lvl) => (
              <Stack key={lvl} direction="horizontal" align="center" gap="tight">
                <Stack
                  style={{
                    width: '6px',
                    height: '6px',
                    borderRadius: '2px',
                    flexShrink: 0,
                    background: LEVEL_CHIP[lvl].color,
                  }}
                />
                <Text size="caption" color="subtle">
                  {stats[lvl]} {lvl.toLowerCase()}
                </Text>
              </Stack>
            ))}
          </Stack>

          {/* Log stream */}
          <CardContent flush>
            <Stack ref={scrollRef} overflowY="auto" gap="none" style={{ maxHeight: '560px' }}>
              {filtered.length === 0 ? (
                <Stack align="center" gap="snug" style={{ padding: '3rem 1rem' }}>
                  <Stack style={{ color: 'oklch(var(--color-foreground-subtle) / 1)' }}>
                    <Icon name="search-x" size="sm" />
                  </Stack>
                  <Text size="supporting" color="muted">
                    No log lines match your filter.
                  </Text>
                </Stack>
              ) : (
                filtered.map((line, i) => (
                  <Stack
                    key={i}
                    direction="horizontal"
                    align="baseline"
                    gap="snug"
                    style={{
                      padding: '5px 16px',
                      background: LEVEL_ROW_BG[line.level],
                      borderBottom:
                        i < filtered.length - 1
                          ? '1px solid oklch(var(--color-border) / 0.35)'
                          : undefined,
                    }}
                  >
                    {/* Timestamp */}
                    <Text
                      mono
                      size="caption"
                      color="subtle"
                      style={{ minWidth: '5rem', flexShrink: 0 }}
                    >
                      {line.time}
                    </Text>

                    {/* Level chip */}
                    <Stack
                      align="center"
                      justify="center"
                      style={{
                        padding: '1px 5px',
                        borderRadius: '3px',
                        minWidth: '2.6rem',
                        flexShrink: 0,
                        background: LEVEL_CHIP[line.level].bg,
                      }}
                    >
                      <Text
                        mono
                        size="caption"
                        style={{
                          fontSize: '10px',
                          fontWeight: 600,
                          letterSpacing: '0.04em',
                          color: LEVEL_CHIP[line.level].color,
                        }}
                      >
                        {line.level}
                      </Text>
                    </Stack>

                    {/* Message */}
                    <Text
                      mono
                      size="caption"
                      style={{
                        color:
                          line.level === 'ERR'
                            ? LEVEL_CHIP.ERR.color
                            : line.level === 'WARN'
                              ? LEVEL_CHIP.WARN.color
                              : line.level === 'APPR'
                                ? LEVEL_CHIP.APPR.color
                                : 'oklch(var(--color-foreground-muted) / 1)',
                        lineHeight: 1.7,
                      }}
                    >
                      {line.message}
                    </Text>
                  </Stack>
                ))
              )}
            </Stack>
          </CardContent>
        </Card>
      </Stack>
    </>
  );
}
