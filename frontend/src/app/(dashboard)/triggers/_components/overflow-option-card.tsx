'use client';

/**
 * @tier 3 — triggers-scoped. One choice in the Delivery section's overflow group:
 * a title, an optional Recommended badge, a radio at the trailing edge and a short
 * description. Hades' RadioCard has no badge slot and sits on the darkest surface,
 * so this composes the same parts on the elevated step. Must be rendered inside a
 * `RadioGroup`.
 */

import { Badge, Card, CardContent, RadioGroupItem, Stack, Text } from '@runkora/hades';

export function OverflowOptionCard({
  value,
  selected,
  title,
  description,
  recommended = false,
  disabled = false,
  onSelect,
}: {
  value: string;
  selected: boolean;
  title: string;
  description: string;
  recommended?: boolean;
  disabled?: boolean;
  onSelect: (value: string) => void;
}) {
  return (
    <Card
      subtle
      interactive={!disabled}
      selected={selected}
      column
      onClick={disabled ? undefined : () => onSelect(value)}
      aria-disabled={disabled || undefined}
    >
      <CardContent padded compact grow>
        <Stack gap="snug">
          {/* Top-aligned so the radio stays on the title's first line when the badge wraps. */}
          <Stack direction="horizontal" align="start" gap="snug">
            <Stack direction="horizontal" align="center" gap="snug" grow minWidth="none" wrap>
              <Text as="span" size="supporting" weight="heavy">
                {title}
              </Text>
              {recommended && <Badge size="sm">Recommended</Badge>}
            </Stack>
            <RadioGroupItem value={value} aria-label={title} disabled={disabled} />
          </Stack>
          <Text size="caption" color="muted">
            {description}
          </Text>
        </Stack>
      </CardContent>
    </Card>
  );
}
