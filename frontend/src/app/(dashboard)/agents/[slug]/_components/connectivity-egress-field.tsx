'use client';

/** @tier 3 — agents/[slug]-scoped. Compact egress posture field for the
 * Connectivity tab: the mode selector plus, in whitelist mode, the chip-based
 * host editor (reused from Environments). Replaces the create form's raw
 * textarea with add/remove chips. */

import { FormField, NativeSelect, Stack, Text } from '@orbit/terra';
import type { ConnectivityMode } from '@orbit/shared/api';
import { EgressHostsEditor } from '../../../environments/_components/egress-hosts-editor';

const MODE_HINTS: Record<ConnectivityMode, string> = {
  none: 'No outbound internet access. The agent can still reach platform services.',
  whitelist: 'Outbound access only to the hosts you list below.',
  open: 'Outbound access to any host. Credentialed destinations are still gated by connections.',
};

interface ConnectivityEgressFieldProps {
  mode: ConnectivityMode;
  /** Whitelist hosts as an array (one entry per host pattern). */
  hosts: string[];
  onModeChange: (mode: ConnectivityMode) => void;
  onHostsChange: (hosts: string[]) => void;
}

export function ConnectivityEgressField({
  mode,
  hosts,
  onModeChange,
  onHostsChange,
}: ConnectivityEgressFieldProps) {
  return (
    <Stack gap="snug">
      <Stack maxWidth="form-field" minWidth="none">
        <FormField id="agent-egress-mode" label="Egress mode" hint={MODE_HINTS[mode]}>
          <NativeSelect
            id="agent-egress-mode"
            value={mode}
            onChange={(e) => onModeChange(e.target.value as ConnectivityMode)}
          >
            <option value="none">No egress</option>
            <option value="whitelist">Whitelist</option>
            <option value="open">Open</option>
          </NativeSelect>
        </FormField>
      </Stack>

      {mode === 'whitelist' && (
        <FormField id="agent-egress-host-input" label="Allowed hosts">
          <EgressHostsEditor
            value={hosts}
            onChange={onHostsChange}
            inputId="agent-egress-host-input"
            showCommonRegistries={false}
          />
        </FormField>
      )}

      {mode === 'whitelist' && hosts.length === 0 && (
        <Text size="supporting" color="muted">
          No hosts listed yet — this agent will have no egress until you add at least one.
        </Text>
      )}
    </Stack>
  );
}
