'use client';

/** @tier 3 — triggers-scoped. Phase one: choose what starts the run. Rendered
 * entirely from the kind registry. */

import * as React from 'react';
import { Card, CardContent, Icon, IconTile, Separator, Stack, Text } from '@orbit/terra';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import type { TriggerKind } from './types';

export interface KindPickerProps {
  onSelect: (kind: TriggerKind) => void;
  /** Receives the first kind card so the wizard can land focus in the content. */
  firstCardRef?: React.Ref<HTMLDivElement>;
}

/**
 * The choice here is "what wakes this up", never "what does it run". The target
 * is chosen later and is a separate concern.
 */
export function KindPicker({ onSelect, firstCardRef }: KindPickerProps) {
  return (
    <Stack gap="relaxed">
      <Text size="supporting" color="muted">
        Choose what starts the run. Every trigger targets one agent and is policy-checked before it
        fires.
      </Text>

      <Stack gap="cozy">
        {KIND_ORDER.map((id, index) => {
          const def = TRIGGER_KINDS[id];
          return (
            // Keyboard parity with the mouse path: the card is a real button
            // to the accessibility tree, reachable by Tab and fired by
            // Enter/Space, and the first one receives initial focus.
            <Card
              key={id}
              ref={index === 0 ? firstCardRef : undefined}
              interactive
              role="button"
              tabIndex={0}
              aria-label={`Create a ${def.title.toLowerCase()} trigger`}
              onClick={() => onSelect(def.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(def.id);
                }
              }}
            >
              <CardContent padded compact>
                <Stack direction="horizontal" align="center" gap="cozy">
                  <IconTile tone="accent">
                    <Icon name={def.icon} size="xs" label="" />
                  </IconTile>
                  <Stack gap="tight" grow minWidth="none">
                    <Text as="span" size="supporting" weight="heavy">
                      {def.title}
                    </Text>
                    <Text size="caption" color="muted">
                      {def.description}
                    </Text>
                    <Text size="micro" mono color="subtle">
                      {def.example}
                    </Text>
                  </Stack>
                  <Icon name="chevron-right" size="xs" color="subtle" shrink label="" />
                </Stack>
              </CardContent>
            </Card>
          );
        })}
      </Stack>

      <Stack gap="snug">
        <Text size="caption" weight="heavy" color="muted">
          Coming soon
        </Text>
        <Text size="caption" color="subtle">
          Each is another way for something to start. Target and delivery settings work the same for
          all of them.
        </Text>
        <Card subtle>
          <CardContent flush>
            {PLANNED_KINDS.map((k, i) => (
              <Stack key={k.title} gap="none" disabled>
                {i > 0 && <Separator span="full" />}
                <Stack direction="horizontal" align="center" gap="cozy" padding="cozy">
                  <Icon name={k.icon} size="xs" color="subtle" shrink label="" />
                  <Text as="span" size="caption" weight="strong" color="muted">
                    {k.title}
                  </Text>
                  <Text as="span" size="caption" color="subtle" truncate>
                    {k.description}
                  </Text>
                </Stack>
              </Stack>
            ))}
          </CardContent>
        </Card>
      </Stack>
    </Stack>
  );
}
