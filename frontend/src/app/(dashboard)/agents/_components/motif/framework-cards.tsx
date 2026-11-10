'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { Grid, RadioCard, RadioGroup } from '@runkora/hades';
import { AGENT_FRAMEWORKS, FRAMEWORK_LABELS } from '@runkora/shared/agent-frameworks.generated';
import type { AgentFramework } from '@runkora/shared/api';

export { FRAMEWORK_LABELS };

export function FrameworkCards({
  value,
  onChange,
  disabled = false,
}: {
  value: AgentFramework;
  onChange: (v: AgentFramework) => void;
  /** Read-only, for callers who may not change the agent. */
  disabled?: boolean;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as AgentFramework)}
      aria-label="Framework"
    >
      <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="cozy">
        {AGENT_FRAMEWORKS.map((f) => (
          <RadioCard
            key={f.id}
            value={f.id}
            selected={value === f.id}
            onSelect={(v) => onChange(v as AgentFramework)}
            icon="square-terminal"
            title={f.label}
            description={f.description}
            disabled={disabled}
          />
        ))}
      </Grid>
    </RadioGroup>
  );
}
