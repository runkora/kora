'use client';

/** @tier 3 — triggers-scoped. A toggle card on the elevated step: Hades' ToggleRow
 * always sits on the darkest one, and the trigger modal's cards are a shade lighter.
 * The title and its line of context lead; the switch sits at the trailing edge.
 * Promote to src/components/ if a second feature needs it. */

import { Card, Stack, Text, Toggle } from '@runkora/hades';

export function PlainToggleRow({
  label,
  description,
  checked,
  onChange,
  disabled = false,
}: {
  label: string;
  /** One line of supporting context under the label. */
  description?: string;
  checked: boolean;
  onChange: (next: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <Stack disabled={disabled}>
      <Card
        subtle
        border="subtle"
        interactive={!disabled}
        // The whole card is the hit target, like a label around a checkbox. The switch
        // handles its own click, so a click that started on it is not toggled twice.
        onClick={(e) => {
          if (disabled || (e.target as HTMLElement).closest('[role="switch"]')) return;
          onChange(!checked);
        }}
      >
        <Stack
          direction="horizontal"
          align="center"
          gap="comfortable"
          paddingInline="comfortable"
          paddingBlock="cozy"
        >
          <Stack gap="tight" grow minWidth="none">
            <Text as="span" size="supporting" weight="heavy">
              {label}
            </Text>
            {description !== undefined && (
              <Text size="caption" color="muted">
                {description}
              </Text>
            )}
          </Stack>
          <Toggle checked={checked} onChange={onChange} disabled={disabled} aria-label={label} />
        </Stack>
      </Card>
    </Stack>
  );
}
