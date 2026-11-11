'use client';

/**
 * @tier 3 — run-page-scoped. Inline banner that surfaces SSE connection issues
 * so the user knows when the stream is degraded or terminally closed and can
 * manually retry. Renders nothing when the connection is healthy.
 */

import { Button, Stack, Text } from '@runkora/hades';
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
      gap="cozy"
      shrink
      style={{
        padding: '6px 14px',
        background: isClosed
          ? 'color-mix(in oklch, var(--color-status-error) 18%, transparent)'
          : 'color-mix(in oklch, var(--color-status-degraded) 18%, transparent)',
        borderBottom: '1px solid var(--color-border-subtle)',
      }}
      role="status"
      aria-live="polite"
    >
      <Text as="span" size="caption" color="foreground">
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
