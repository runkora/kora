'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import {
  Card,
  CardContent,
  Grid,
  Icon,
  RadioGroup,
  RadioGroupItem,
  Stack,
  Text,
} from '@orbit/terra';
import type { AgentFramework } from '@orbit/shared/api';

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
}: {
  value: AgentFramework;
  onChange: (v: AgentFramework) => void;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as AgentFramework)}
      aria-label="Framework"
    >
      <Grid cols={3} gap="cozy">
        {FRAMEWORKS.map((f) => {
          const selected = value === f.id;
          return (
            <Card key={f.id} interactive selected={selected} onClick={() => onChange(f.id)}>
              <CardContent padded compact>
                <Stack gap="snug">
                  <Stack direction="horizontal" align="center">
                    <Icon
                      name="square-terminal"
                      size="sm"
                      color={selected ? 'accent' : 'muted'}
                      container
                      containerVariant={selected ? 'accent' : 'surface-subtle'}
                      label=""
                    />
                    <Stack grow />
                    <RadioGroupItem value={f.id} aria-label={f.name} />
                  </Stack>
                  <Stack gap="tight">
                    <Text as="span" size="supporting" weight="heavy">
                      {f.name}
                    </Text>
                    <Text size="micro" color="muted">
                      {f.desc}
                    </Text>
                  </Stack>
                </Stack>
              </CardContent>
            </Card>
          );
        })}
      </Grid>
    </RadioGroup>
  );
}
