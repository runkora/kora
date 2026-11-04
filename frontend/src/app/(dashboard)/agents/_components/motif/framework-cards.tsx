'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import { Grid, RadioCard, RadioGroup } from '@runkora/hades';
import type { AgentFramework } from '@runkora/shared/api';

export const FRAMEWORK_LABELS: Record<AgentFramework, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
  opencode: 'Open Code',
};

const FRAMEWORKS: { id: AgentFramework; name: string; desc: string }[] = [
  {
    id: 'claude-code',
    name: FRAMEWORK_LABELS['claude-code'],
    desc: "Anthropic's agentic coding harness.",
  },
  { id: 'codex', name: FRAMEWORK_LABELS.codex, desc: "OpenAI's coding agent runtime." },
  { id: 'opencode', name: FRAMEWORK_LABELS.opencode, desc: 'Open-source, model-agnostic agent.' },
];

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
      <Grid cols={3} gap="cozy">
        {FRAMEWORKS.map((f) => (
          <RadioCard
            key={f.id}
            value={f.id}
            selected={value === f.id}
            onSelect={(v) => onChange(v as AgentFramework)}
            icon="square-terminal"
            title={f.name}
            description={f.desc}
            disabled={disabled}
          />
        ))}
      </Grid>
    </RadioGroup>
  );
}
