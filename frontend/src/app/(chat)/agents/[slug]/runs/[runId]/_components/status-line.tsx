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

/**
 * The status as what is happening and, apart, how long it has taken. Only the
 * first is announced to screen readers; the second changes every second.
 */
export function statusParts(
  activity: LiveActivity,
  now: number,
): { label: string; elapsed: string } | null {
  switch (activity.kind) {
    case 'retry': {
      const reason = RETRY_REASONS[activity.error];
      return {
        label: `Model provider error${reason ? ` (${reason})` : ''}, retrying (${activity.attempt}/${activity.maxRetries})`,
        elapsed: '',
      };
    }
    case 'sandbox':
      return {
        label: 'Starting sandbox…',
        elapsed: formatDuration(Math.max(0, now - activity.since)),
      };
    case 'tool': {
      const d = describeTool(activity.tool.name, activity.tool.input);
      // The runtime's own count wins after a reload; between reports the clock ticks on.
      const ms = Math.max((activity.tool.progressSeconds ?? 0) * 1000, now - activity.since);
      return {
        label: `${d.label}${d.target ? ` ${d.target}` : ''} ·`,
        elapsed: formatDuration(Math.max(0, ms)),
      };
    }
    case 'thinking': {
      const ms = Math.max(0, now - activity.since);
      return ms < THINKING_AFTER_MS ? null : { label: 'Thinking…', elapsed: formatDuration(ms) };
    }
  }
}

export function statusText(activity: LiveActivity, now: number): string | null {
  const parts = statusParts(activity, now);
  if (!parts) return null;
  return parts.elapsed ? `${parts.label} ${parts.elapsed}` : parts.label;
}

export function StatusLine({ activity }: { activity: LiveActivity }) {
  const now = useNow(true);
  const parts = statusParts(activity, now);
  return (
    <Stack
      direction="horizontal"
      align="center"
      data-status-line=""
      style={{ gap: 10, padding: '12px 20px' }}
    >
      <OrbitalLoader size="sm" />
      {parts && (
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
          <Text as="span" role="status">
            {parts.label}
          </Text>
          {parts.elapsed && (
            <Text as="span" aria-hidden>
              {' '}
              {parts.elapsed}
            </Text>
          )}
        </Text>
      )}
    </Stack>
  );
}
