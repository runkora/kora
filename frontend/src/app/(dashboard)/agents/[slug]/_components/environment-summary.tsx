'use client';

/** @tier 3 — agents/[slug]-scoped. Compact summary of a selected Environment's
 * contents for the Connectivity tab's Environment layer card: a counts line, a
 * few egress-host chips (first N + a "+N more" chip) and connector badges. Host
 * chips mirror the Effective-access panel's `<Code>` styling so chips read
 * consistently across the tab. Pure presentation — it renders the already-loaded
 * `Environment` object and never fetches. */

import { Badge, Code, Icon, Stack, Text } from '@orbit/terra';
import type { Environment } from '@orbit/shared/api';

// How many host chips to show before collapsing the remainder into "+N more".
const MAX_HOST_CHIPS = 5;

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

interface EnvironmentSummaryProps {
  environment: Environment;
}

export function EnvironmentSummary({ environment }: EnvironmentSummaryProps) {
  const hosts = environment.egress_hosts;
  const connectors = environment.connectors;
  const shownHosts = hosts.slice(0, MAX_HOST_CHIPS);
  const overflow = hosts.length - shownHosts.length;

  return (
    <Stack
      gap="snug"
      paddingBlock="snug"
      paddingInline="comfortable"
      minWidth="none"
      style={{
        border: '1px solid var(--color-border)',
        borderRadius: '6px',
        background: 'var(--color-surface-elevated)',
      }}
    >
      {/* Identity + counts */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <Stack direction="horizontal" align="center" gap="tight" minWidth="none">
          <Icon name="layers" size="xs" color="subtle" />
          <Text size="supporting" weight="emphasis">
            {environment.name}
          </Text>
        </Stack>
        <Text size="caption" color="muted">
          {plural(hosts.length, 'egress host', 'egress hosts')} ·{' '}
          {plural(connectors.length, 'connector', 'connectors')}
        </Text>
      </Stack>

      {/* Egress host chips — first N + "+N more". */}
      {hosts.length > 0 && (
        <Stack direction="horizontal" gap="tight" wrap>
          {shownHosts.map((host) => (
            <Code key={host}>{host}</Code>
          ))}
          {overflow > 0 && (
            <Badge variant="dim" size="sm">
              +{overflow} more
            </Badge>
          )}
        </Stack>
      )}

      {/* Connector badges — the connection names. */}
      {connectors.length > 0 && (
        <Stack direction="horizontal" gap="tight" wrap>
          {connectors.map((c) => (
            <Badge key={c.connection} variant="secondary" size="sm">
              {c.connection}
            </Badge>
          ))}
        </Stack>
      )}
    </Stack>
  );
}
