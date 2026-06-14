'use client';

/** @tier 3 — agents-scoped. Environment selector: binds an agent type to one of
 * the tenant's environments (or "None"). The environment's egress hosts and
 * connectors are folded into the agent's effective access server-side. Shared by
 * the create form and the settings tab. */

import { useEffect, useRef, useState } from 'react';
import { FormField, NativeSelect, Text } from '@orbit/terra';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { Environment } from '@orbit/shared/api';

interface EnvironmentFieldProps {
  /** Selected environment id, or "" for None. */
  value: string;
  onChange: (environmentId: string) => void;
  /**
   * Optional. Fired once the tenant's environments have loaded, surfacing the
   * full `Environment` objects so a caller can render a summary of the selected
   * environment's hosts/connectors without issuing a second request. The
   * `value`/`onChange(id)` contract is unaffected — this is purely additive.
   */
  onEnvironmentsLoaded?: (environments: Environment[]) => void;
  /**
   * Optional. When true, renders only the bare select (with an accessible
   * label) and drops the `FormField` label + hint. Use where the surrounding
   * context already names and describes the environment layer — e.g. the
   * Connectivity tab's Environment card header — so the copy is not duplicated.
   */
  compact?: boolean;
}

export function EnvironmentField({
  value,
  onChange,
  onEnvironmentsLoaded,
  compact = false,
}: EnvironmentFieldProps) {
  const [environments, setEnvironments] = useState<Environment[]>([]);

  // Keep the callback in a ref so it does not have to be a stable dependency of
  // the fetch effect — the field fetches exactly once on mount regardless of how
  // the caller declares its handler. The ref is synced in an effect (never during
  // render) so a fresh handler is always available by the time the fetch resolves.
  const onLoadedRef = useRef(onEnvironmentsLoaded);
  useEffect(() => {
    onLoadedRef.current = onEnvironmentsLoaded;
  }, [onEnvironmentsLoaded]);

  useEffect(() => {
    fetch(ApiRoute.environments.list(), { cache: 'no-store' })
      .then((r) => r.json())
      .then((body: { data?: Environment[] }) => {
        const loaded = body.data ?? [];
        setEnvironments(loaded);
        onLoadedRef.current?.(loaded);
      })
      .catch(() => undefined);
  }, []);

  const control =
    environments.length === 0 ? (
      <Text size="supporting" color="muted">
        No environments are defined for this tenant. Create one on the Environments page to attach a
        shared bundle of egress hosts and connectors.
      </Text>
    ) : (
      <NativeSelect
        id="agent-environment"
        aria-label="Environment"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">None</option>
        {environments.map((env) => (
          <option key={env.id} value={env.id}>
            {env.name}
          </option>
        ))}
      </NativeSelect>
    );

  // Compact mode: bare control, no label/hint — the surrounding card supplies
  // the name and description, so repeating them here would duplicate copy.
  if (compact) return control;

  return (
    <FormField
      id="agent-environment"
      label="Environment"
      hint="Optional. Folds the selected environment's egress hosts and connectors into this agent's effective access."
    >
      {control}
    </FormField>
  );
}
