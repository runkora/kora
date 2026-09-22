'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { Card, CardContent, Icon, NativeSelect, Show, Stack, Text } from '@orbit/terra';
import type { Environment } from '@orbit/shared/api';

export function EnvironmentCard({
  environments,
  value,
  onChange,
}: {
  environments: Environment[];
  value: string;
  onChange: (id: string) => void;
}) {
  const env = environments.find((e) => e.id === value);
  return (
    <Card selected={!!value} selectedVariant="outline">
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <Icon
            name="layers"
            size="sm"
            color={value ? 'accent' : 'muted'}
            container
            containerVariant={value ? 'accent' : 'surface-subtle'}
            label=""
          />
          <Stack grow gap="tight">
            <Text as="span" size="supporting" weight="heavy">
              Environment
            </Text>
            <Text as="span" size="micro" color="subtle">
              {env
                ? `Merges ${env.egress_hosts.length} hosts and ${env.connectors.length} connections into effective access.`
                : 'No environment. The agent has exactly the access configured above.'}
            </Text>
          </Stack>
          <Show below="lg">
            <Stack grow>
              <NativeSelect
                value={value}
                onChange={(e) => onChange(e.target.value)}
                aria-label="Environment"
              >
                <option value="">None</option>
                {environments.map((e) => (
                  <option key={e.id} value={e.id}>
                    {e.name}
                  </option>
                ))}
              </NativeSelect>
            </Stack>
          </Show>
          <Show above="lg">
            <Stack minWidth="form-field" maxWidth="form-field">
              <NativeSelect
                value={value}
                onChange={(e) => onChange(e.target.value)}
                aria-label="Environment"
              >
                <option value="">None</option>
                {environments.map((e) => (
                  <option key={e.id} value={e.id}>
                    {e.name}
                  </option>
                ))}
              </NativeSelect>
            </Stack>
          </Show>
        </Stack>
      </CardContent>
    </Card>
  );
}
