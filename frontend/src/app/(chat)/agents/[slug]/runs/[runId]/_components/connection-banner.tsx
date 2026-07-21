'use client';

/**
 * @tier 3 — run-chat-scoped. Inline banner that surfaces SSE connection issues
 * so the user knows when the stream is degraded or terminally closed and can
 * manually retry. Renders nothing when the connection is healthy.
 */

import { Button, Stack, Text } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

export function ConnectionBanner() {
  const { connectionStatus, reconnect } = useRunContext();

  if (connectionStatus === 'open' || connectionStatus === 'connecting') {
    return null;
  }

  const isClosed = connectionStatus === 'closed';
  const message = isClosed
    ? 'Connection lost. The agent may still be running. Reconnect to resume the stream.'
    : 'Reconnecting to the agent stream…';

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      style={{
        gap: 12,
        padding: '6px 14px',
        flexShrink: 0,
        background: isClosed
          ? 'color-mix(in oklch, var(--chat-error) 18%, transparent)'
          : 'color-mix(in oklch, var(--chat-degraded) 18%, transparent)',
        borderBottom: '1px solid var(--chat-bds)',
        fontSize: 12,
        color: 'var(--chat-fg)',
      }}
      role="status"
      aria-live="polite"
    >
      <Text as="span" style={{ fontSize: 12, color: 'var(--chat-fg)' }}>
        {message}
      </Text>
      {isClosed && (
        <Button variant="outline" size="sm" onClick={reconnect}>
          Reconnect
        </Button>
      )}
    </Stack>
  );
}
