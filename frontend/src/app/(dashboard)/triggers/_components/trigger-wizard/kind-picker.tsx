'use client';

/** @tier 3 — triggers-scoped. Phase one of the wizard: choose what starts the
 * run. Rendered entirely from the kind registry. */

import { Badge, Grid, RadioCard, RadioGroup, Stack, Text } from '@orbit/hades';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import type { TriggerKind } from './types';

export interface KindPickerProps {
  onSelect: (kind: TriggerKind) => void;
}

/**
 * The choice here is "what wakes this up", never "what does it run". The target
 * is chosen later and is a separate concern.
 */
export function KindPicker({ onSelect }: KindPickerProps) {
  return (
    <Stack gap="relaxed">
      <Stack gap="snug">
        <Text weight="heavy">Available now</Text>
        {/* RadioCard needs the Radix group context even for a pick-and-go grid;
            value stays empty because choosing advances the flow rather than
            persisting a selection. */}
        <RadioGroup value="" aria-label="Trigger kind">
          <Grid cols={3} gap="comfortable">
            {KIND_ORDER.map((id) => {
              const def = TRIGGER_KINDS[id];
              return (
                <Stack key={id} gap="tight">
                  <RadioCard
                    value={def.id}
                    selected={false}
                    onSelect={(v) => onSelect(v as TriggerKind)}
                    icon={def.icon}
                    title={def.title}
                    description={def.description}
                  />
                  <Text size="micro" mono color="subtle" truncate>
                    {def.example}
                  </Text>
                </Stack>
              );
            })}
          </Grid>
        </RadioGroup>
      </Stack>

      <Stack gap="snug">
        <Stack direction="horizontal" gap="snug" align="center">
          <Text weight="heavy">Planned</Text>
          <Badge variant="outline">Soon</Badge>
        </Stack>
        <Text size="supporting" color="muted">
          Each is another way for something to start. Nothing downstream changes, which is the test
          of whether this design is right.
        </Text>
        <RadioGroup value="" aria-label="Planned trigger kinds">
          <Grid cols={3} gap="comfortable">
            {PLANNED_KINDS.map((k) => (
              <RadioCard
                key={k.title}
                value={k.title}
                selected={false}
                disabled
                onSelect={() => undefined}
                icon={k.icon}
                title={k.title}
                description={k.description}
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>
    </Stack>
  );
}
