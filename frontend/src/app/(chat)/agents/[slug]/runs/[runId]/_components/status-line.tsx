'use client';

/**
 * @tier 3 — run-chat-scoped.
 * One line saying what the agent is doing while nothing visible streams:
 * starting a sandbox, retrying a provider error, running a tool, or thinking.
 */

import { Stack, Text, OrbitalLoader } from '@orbit/terra';
import type { LiveActivity } from '../_types';
import { describeTool, formatDuration } from '../_lib/tool-activity';
import { useNow } from '../_hooks/use-now';

// Below this, "Thinking" flickers between events and says nothing useful.
const THINKING_AFTER_MS = 3000;

const RETRY_REASONS: Record<string, string> = {
  authentication_failed: 'authentication failed',
  rate_limit: 'rate limited',
  overloaded: 'overloaded',
  server_error: 'server error',
};

export function statusText(activity: LiveActivity, now: number): string | null {
  switch (activity.kind) {
    case 'retry': {
      const reason = RETRY_REASONS[activity.error];
      return `Model provider error${reason ? ` (${reason})` : ''}, retrying (${activity.attempt}/${activity.maxRetries})`;
    }
    case 'sandbox':
      return `Starting sandbox… ${formatDuration(Math.max(0, now - activity.since))}`;
    case 'tool': {
      const d = describeTool(activity.tool.name, activity.tool.input);
      // The runtime's own count wins after a reload; between reports the clock ticks on.
      const elapsed = Math.max((activity.tool.progressSeconds ?? 0) * 1000, now - activity.since);
      return `${d.label}${d.target ? ` ${d.target}` : ''} · ${formatDuration(Math.max(0, elapsed))}`;
    }
    case 'thinking': {
      const elapsed = Math.max(0, now - activity.since);
      return elapsed < THINKING_AFTER_MS ? null : `Thinking… ${formatDuration(elapsed)}`;
    }
  }
}

export function StatusLine({ activity }: { activity: LiveActivity }) {
  const now = useNow(true);
  const text = statusText(activity, now);
  return (
    <Stack
      direction="horizontal"
      align="center"
      role="status"
      data-status-line=""
      style={{ gap: 10, padding: '12px 20px' }}
    >
      <OrbitalLoader size="sm" />
      {text && (
        <Text
          as="span"
          style={{
            fontSize: 13,
            color:
              activity.kind === 'retry' ? 'var(--color-status-degraded)' : 'var(--color-muted)',
            fontFamily: activity.kind === 'tool' ? 'var(--font-mono)' : undefined,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            minWidth: 0,
          }}
        >
          {text}
        </Text>
      )}
    </Stack>
  );
}
