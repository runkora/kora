'use client';

/** @tier 3 — agents-scoped. Attaches environments to an agent and marks the
 * default. Used by the builder wizard and the detail Configuration tab. */

import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  Checkbox,
  Icon,
  Stack,
  Text,
} from '@clear-route/hades';
import { useBrand } from '@orbit/theme/brand-provider';
import type { Environment } from '@orbit/shared/api';
import { sizeLabel } from '@orbit/lib/environments';
import { setDefaultEnvironment, toggleEnvironment } from '../../_types';
import type { AgentEnvironment } from '../../_types';

export function EnvironmentsCard({
  environments,
  value,
  onChange,
  loading = false,
  error,
}: {
  /** Every environment in the tenant. */
  environments: Environment[];
  /** The agent's attached environments. */
  value: AgentEnvironment[];
  onChange: (next: AgentEnvironment[]) => void;
  /** True until the tenant's environments load. Until then every attached environment would look deleted. */
  loading?: boolean;
  /** Set when the tenant's environments failed to load. */
  error?: string | null;
}) {
  const { productName } = useBrand();
  const attached = new Map(value.map((e) => [e.environment_id, e.is_default]));
  // A restored draft can hold a deleted environment. Listing it lets the user detach it.
  const missing = value.filter((v) => !environments.some((e) => e.id === v.environment_id));
  const summary =
    value.length === 0
      ? `None attached. The agent runs on the ${productName} base image at the default size.`
      : value.length === 1
        ? 'The agent runs in this environment.'
        : `${value.length} attached. The agent starts in the default and can switch to another.`;

  return (
    <Card selected={value.length > 0} selectedVariant="outline">
      <CardContent padded compact>
        <Stack gap="cozy">
          <Stack direction="horizontal" align="center" gap="cozy">
            <Icon
              name="layers"
              size="sm"
              color={value.length > 0 ? 'accent' : 'muted'}
              container
              containerVariant={value.length > 0 ? 'accent' : 'surface-subtle'}
              label=""
            />
            <Stack grow gap="tight">
              <Text as="span" size="supporting" weight="heavy">
                Environments
              </Text>
              <Text as="span" size="micro" color="subtle">
                {summary}
              </Text>
            </Stack>
          </Stack>

          {loading ? (
            <Text size="supporting" color="muted">
              Loading environments.
            </Text>
          ) : error ? (
            <Alert variant="error">Environments failed to load: {error}</Alert>
          ) : environments.length === 0 && missing.length === 0 ? (
            <Text size="supporting" color="muted">
              No environments exist yet. Create one on the Environments page.
            </Text>
          ) : (
            <Stack gap="snug">
              {environments.map((env) => {
                const isAttached = attached.has(env.id);
                const isDefault = attached.get(env.id) === true;
                return (
                  <Stack key={env.id} direction="horizontal" align="center" gap="cozy">
                    <Stack grow gap="none">
                      <Checkbox
                        checked={isAttached}
                        onChange={() => onChange(toggleEnvironment(value, env.id))}
                      >
                        {env.name}
                      </Checkbox>
                      <Text size="micro" color="subtle">
                        {[env.sandbox_image, sizeLabel(env.size)].filter(Boolean).join(' · ')}
                      </Text>
                    </Stack>
                    {isDefault && value.length > 1 && (
                      <Badge variant="secondary" size="sm">
                        Default
                      </Badge>
                    )}
                    {isAttached && !isDefault && (
                      <Button
                        variant="ghost"
                        size="xs"
                        onClick={() => onChange(setDefaultEnvironment(value, env.id))}
                        aria-label={`Make ${env.name} the default`}
                      >
                        Make default
                      </Button>
                    )}
                  </Stack>
                );
              })}
              {missing.map((m) => (
                <Stack key={m.environment_id} gap="none">
                  <Checkbox
                    checked
                    onChange={() => onChange(toggleEnvironment(value, m.environment_id))}
                  >
                    Deleted environment
                  </Checkbox>
                  <Text size="micro" color="subtle">
                    It no longer exists. Clear it to save the agent.
                  </Text>
                </Stack>
              ))}
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
