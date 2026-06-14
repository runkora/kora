'use client';

/** @tier 3 — agents-scoped. Connector attachment: pick from the tenant's
 * registered connections and the actions the agent may use through each.
 * Compiles server-side to connection:<name>:<action> grants — the credential
 * parallel to connectivity-fields (which compiles hosts to egress:<host>:connect).
 * Shared by the create form and the settings tab. */

import { Badge, Checkbox, FormField, Stack, Text } from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector } from '@orbit/shared/api';

// Human-readable adapter labels, mirroring the read-only Connections page.
const ADAPTER_LABELS: Record<string, string> = {
  github_app: 'GitHub App',
  passthrough: 'Passthrough',
};

function adapterLabel(adapter: string): string {
  return ADAPTER_LABELS[adapter] ?? adapter;
}

interface ConnectorsFieldsProps {
  /** Registered connections available to attach (GET /api/agents/connectors). */
  available: ConnectionCatalogEntry[];
  /** Currently-attached connectors. */
  value: Connector[];
  onChange: (connectors: Connector[]) => void;
}

export function ConnectorsFields({ available, value, onChange }: ConnectorsFieldsProps) {
  const actionsFor = (connection: string): string[] =>
    value.find((c) => c.connection === connection)?.actions ?? [];

  const toggleAction = (connection: string, action: string, on: boolean) => {
    const current = actionsFor(connection);
    const nextActions = on ? [...current, action] : current.filter((a) => a !== action);
    const others = value.filter((c) => c.connection !== connection);
    // Drop the connector entirely when no action is selected — an empty action
    // list is not a grant.
    onChange(nextActions.length > 0 ? [...others, { connection, actions: nextActions }] : others);
  };

  if (available.length === 0) {
    return (
      <Text size="supporting" color="muted">
        No connections are registered for this tenant. Register one on the egress gateway to grant
        the agent brokered, credential-injected access.
      </Text>
    );
  }

  return (
    <Stack gap="comfortable">
      {available.map((conn) => (
        <Stack key={conn.name} gap="snug">
          {/* Connection identity: name + adapter so connections are distinguishable. */}
          <Stack direction="horizontal" gap="tight" align="center">
            <Text size="supporting" weight="emphasis">
              {conn.name}
            </Text>
            <Badge variant="secondary" size="sm">
              {adapterLabel(conn.adapter)}
            </Badge>
          </Stack>
          <FormField
            id={`connector-${conn.name}`}
            label="Actions"
            hint="Actions this agent may use through this connection"
          >
            {conn.actions.length === 0 ? (
              <Text size="supporting" color="muted">
                No grantable actions for this connection.
              </Text>
            ) : (
              <Stack gap="snug">
                {conn.actions.map((action) => (
                  <Checkbox
                    key={action}
                    checked={actionsFor(conn.name).includes(action)}
                    onChange={(checked) => toggleAction(conn.name, action, checked)}
                  >
                    {action}
                  </Checkbox>
                ))}
              </Stack>
            )}
          </FormField>
        </Stack>
      ))}
    </Stack>
  );
}
