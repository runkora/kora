'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { Badge, Grid, RadioCard, RadioGroup, Stack, Text, Textarea } from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import type { ConnectivityMode } from '@runkora/shared/api';

const EGRESS_MODES: { id: ConnectivityMode; name: string; desc: string; icon: IconName }[] = [
  {
    id: 'none',
    name: 'None',
    desc: 'No outbound internet access. The agent can still reach platform services.',
    icon: 'lock',
  },
  {
    id: 'whitelist',
    name: 'Allowlist',
    desc: 'Outbound access only to the hosts you list.',
    icon: 'network',
  },
  {
    id: 'open',
    name: 'Open',
    desc: 'Any host. Credentialed destinations are still gated by connections.',
    icon: 'globe',
  },
];

export function EgressModeCards({
  mode,
  hosts,
  onModeChange,
  onHostsChange,
  disabled = false,
}: {
  mode: ConnectivityMode;
  hosts: string;
  onModeChange: (m: ConnectivityMode) => void;
  onHostsChange: (h: string) => void;
  /** Read-only, for callers who may not change the agent. */
  disabled?: boolean;
}) {
  return (
    <Stack gap="cozy">
      <RadioGroup
        value={mode}
        onValueChange={(v) => onModeChange(v as ConnectivityMode)}
        aria-label="Network egress mode"
      >
        <Grid cols={3} gap="cozy">
          {EGRESS_MODES.map((m) => (
            <RadioCard
              key={m.id}
              value={m.id}
              selected={mode === m.id}
              onSelect={(v) => onModeChange(v as ConnectivityMode)}
              icon={m.icon}
              title={m.name}
              description={m.desc}
              disabled={disabled}
            />
          ))}
        </Grid>
      </RadioGroup>
      {mode === 'whitelist' && (
        <Stack gap="tight">
          <Textarea
            value={hosts}
            onChange={(e) => onHostsChange(e.target.value)}
            rows={3}
            disabled={disabled}
            placeholder={'api.github.com\n*.internal.example.com'}
            aria-label="Allowed hosts"
          />
          <Text size="micro" color="subtle">
            One host per line. Exact,{' '}
            <Badge variant="outline" mono size="sm">
              *
            </Badge>
            , or{' '}
            <Badge variant="outline" mono size="sm">
              *.suffix
            </Badge>{' '}
            patterns.
          </Text>
        </Stack>
      )}
    </Stack>
  );
}
