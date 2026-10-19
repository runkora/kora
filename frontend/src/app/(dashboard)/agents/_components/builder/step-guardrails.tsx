'use client';

/** @tier 3 — agents-scoped. Builder step 4: enforced ceilings + the planned block. */

import { Eyebrow, SectionHeading, Stack, Text } from '@runkora/hades';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { GuardrailsFields, type GuardrailValues } from '../motif/guardrail-rows';
import { PlannedLimits } from '../motif/planned-limits';

export function StepGuardrails() {
  const { fields, setField } = useAgentBuilderContext();

  return (
    <Stack gap="relaxed">
      <SectionHeading
        icon="shield"
        title="Guardrails"
        description="Set the ceilings kora enforces at runtime: spend, capacity and loop limits."
      />

      <Stack direction="horizontal" align="baseline" gap="snug">
        <Eyebrow>Limits</Eyebrow>
        <Text as="span" size="micro" color="subtle">
          Every limit is optional. Switch on the ones you want kora to enforce.
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
