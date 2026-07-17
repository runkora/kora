'use client';

/** @tier 3 — agents-scoped. Builder step 4: enforced ceilings + the planned block. */

import { Stack, Text } from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { GuardrailsFields, type GuardrailValues } from '../motif/guardrail-rows';
import { PlannedLimits } from '../motif/planned-limits';
import { StepHeader } from './step-header';

export function StepGuardrails() {
  const { fields, setField } = useAgentBuilderContext();

  return (
    <Stack gap="relaxed">
      <StepHeader
        icon="shield"
        title="Guardrails"
        description="Set the ceilings Orbit enforces at runtime: spend and capacity limits."
      />

      <Stack direction="horizontal" align="baseline" gap="snug">
        <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
          Limits
        </Text>
        <Text as="span" size="micro" color="subtle">
          Every limit is optional. Switch on the ones you want Orbit to enforce.
        </Text>
      </Stack>

      {/* GuardrailValues is a subset of BuilderFields with identical field
          names, so the setter passes straight through. */}
      <GuardrailsFields
        values={fields}
        onChange={(key: keyof GuardrailValues, value: GuardrailValues[keyof GuardrailValues]) =>
          setField(key, value)
        }
      />

      <PlannedLimits />
    </Stack>
  );
}
