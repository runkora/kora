'use client';

/** @tier 3 — agents-scoped. Egress connectivity posture fields (mode + whitelist
 * hosts), shared by the create form and the settings tab. Promote to
 * src/components/ only if a non-agents feature needs it. */

import { FormField, NativeSelect, Stack, Text, Textarea } from '@orbit/hades';
import type { ConnectivityMode } from '@orbit/shared/api';

// hostsToText / textToHosts bridge the API's string[] and the textarea's
// one-host-per-line text. Trimming + empty-line filtering happens on the way in.
export function hostsToText(hosts: string[] | undefined): string {
  return (hosts ?? []).join('\n');
}

export function textToHosts(text: string): string[] {
  return text
    .split('\n')
    .map((h) => h.trim())
    .filter((h) => h.length > 0);
}

const MODE_HINTS: Record<ConnectivityMode, string> = {
  none: 'No outbound internet access. The agent can still reach platform services.',
  whitelist: 'Outbound access only to the hosts you list below.',
  open: 'Outbound access to any host. Credentialed destinations are still gated by connections.',
};

interface ConnectivityFieldsProps {
  mode: ConnectivityMode;
  /** Whitelist hosts as textarea text (one host per line). */
  hosts: string;
  onModeChange: (mode: ConnectivityMode) => void;
  onHostsChange: (hosts: string) => void;
  error?: string;
}

export function ConnectivityFields({
  mode,
  hosts,
  onModeChange,
  onHostsChange,
  error,
}: ConnectivityFieldsProps) {
  return (
    <Stack gap="snug">
      <FormField id="agent-connectivity-mode" label="Egress mode" hint={MODE_HINTS[mode]}>
        <NativeSelect
          id="agent-connectivity-mode"
          value={mode}
          onChange={(e) => onModeChange(e.target.value as ConnectivityMode)}
        >
          <option value="none">No egress</option>
          <option value="whitelist">Whitelist</option>
          <option value="open">Open</option>
        </NativeSelect>
      </FormField>

      {mode === 'whitelist' && (
        <FormField
          id="agent-connectivity-hosts"
          label="Allowed hosts"
          hint="One per line. Exact (api.github.com), subdomain wildcard (*.github.com), or * for any host."
          error={error}
        >
          <Textarea
            id="agent-connectivity-hosts"
            value={hosts}
            onChange={(e) => onHostsChange(e.target.value)}
            placeholder={'api.github.com\n*.internal.acme.com'}
            rows={4}
          />
        </FormField>
      )}

      {mode === 'whitelist' && textToHosts(hosts).length === 0 && (
        <Text size="supporting" color="muted">
          No hosts listed yet - this agent will have no egress until you add at least one.
        </Text>
      )}
    </Stack>
  );
}
