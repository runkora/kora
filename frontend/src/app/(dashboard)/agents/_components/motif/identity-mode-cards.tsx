'use client';

/** @tier 3 — agents-scoped motif: the identity-model choice in the builder.
 * The choice is immutable after creation, so the detail Configuration tab shows
 * the mode read-only rather than reusing these cards. */

import { Grid, RadioCard, RadioGroup } from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import type { AgentIdentityMode } from '@orbit/shared/api';

export const IDENTITY_MODE_LABELS: Record<AgentIdentityMode, string> = {
  autonomous: 'Autonomous',
  delegated: 'Delegated',
};

/** One-line summary shown where the mode is displayed read-only. */
export const IDENTITY_MODE_SUMMARIES: Record<AgentIdentityMode, string> = {
  autonomous: 'Autonomous: acts as itself, with its own access',
  delegated: 'Delegated: acts on behalf of the person who runs it',
};

// Each option explains the three properties of the identity model: who the
// agent acts as, who can start it, and who can see its runs. The delegated
// option states the automated-start constraint at the point of choice, not on
// failure (delegated-agent-identity PRD, P1).
const IDENTITY_MODES: { id: AgentIdentityMode; name: string; desc: string; icon: IconName }[] = [
  {
    id: 'autonomous',
    name: IDENTITY_MODE_LABELS.autonomous,
    desc: 'Acts as itself, with its own granted access, whoever starts it. People, triggers, and automated sources can start it. Runs are visible under your tenant’s normal access rules.',
    icon: 'bot',
  },
  {
    id: 'delegated',
    name: IDENTITY_MODE_LABELS.delegated,
    desc: 'Acts on behalf of the person who runs it, limited to what that person can access within the agent’s grants and drawing on their individual budget. Only a person can start it; triggers and automated sources cannot. Each person’s runs are private to them.',
    icon: 'user-check',
  },
];

export function IdentityModeCards({
  value,
  onChange,
}: {
  value: AgentIdentityMode;
  onChange: (v: AgentIdentityMode) => void;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as AgentIdentityMode)}
      aria-label="Identity model"
    >
      <Grid cols={2} gap="cozy">
        {IDENTITY_MODES.map((m) => (
          <RadioCard
            key={m.id}
            value={m.id}
            selected={value === m.id}
            onSelect={(v) => onChange(v as AgentIdentityMode)}
            icon={m.icon}
            title={m.name}
            description={m.desc}
          />
        ))}
      </Grid>
    </RadioGroup>
  );
}
