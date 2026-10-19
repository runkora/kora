'use client';

/** @tier 3 — agents-scoped motif: the identity-model choice in the builder.
 * The choice is immutable after creation, so the detail Configuration tab shows
 * the mode read-only rather than reusing these cards. */

import { Grid, RadioCard, RadioGroup } from '@runkora/hades';
import type { IconName } from '@runkora/hades';
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
    desc: 'Acts on behalf of the person who runs it, limited to their access and spending their budget. Only a person can start it; triggers and automated sources cannot. Each person’s runs are private to them.',
    icon: 'user-check',
  },
];

export function IdentityModeCards({
  value,
  onChange,
  disabledModes,
}: {
  value: AgentIdentityMode;
  onChange: (v: AgentIdentityMode) => void;
  /**
   * Modes the caller may not create, mapped to the inline explanation shown
   * under the card. A disabled card is dimmed and unselectable (the same
   * Stack-disabled treatment as other unavailable builder options); selection
   * changes to a disabled mode are ignored.
   */
  disabledModes?: Partial<Record<AgentIdentityMode, string>>;
}) {
  const select = (v: string) => {
    const mode = v as AgentIdentityMode;
    if (disabledModes?.[mode]) return;
    onChange(mode);
  };
  return (
    <RadioGroup value={value} onValueChange={select} aria-label="Identity model">
      <Grid cols={2} gap="cozy">
        {IDENTITY_MODES.map((m) => {
          const disabledReason = disabledModes?.[m.id];
          return (
            <RadioCard
              key={m.id}
              value={m.id}
              selected={value === m.id}
              onSelect={select}
              icon={m.icon}
              title={m.name}
              description={disabledReason ? `${m.desc} ${disabledReason}` : m.desc}
              disabled={Boolean(disabledReason)}
            />
          );
        })}
      </Grid>
    </RadioGroup>
  );
}
