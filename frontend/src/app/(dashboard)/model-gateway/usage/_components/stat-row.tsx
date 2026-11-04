/**
 * @tier 3 — model-gateway/usage scoped. Renders a labelled value row inside
 * a stat card, with the label left-aligned and value right-aligned in
 * monospace tabular figures.
 * @promote-to-tier-2 if: a second feature area outside model-gateway usage
 * needs this label+value row pattern.
 */
import { Stack, Text } from '@runkora/hades';

export interface StatRowProps {
  label: string;
  value: string;
}

export function StatRow({ label, value }: StatRowProps) {
  return (
    <Stack direction="horizontal" align="center" justify="between" paddingBlock="tight">
      <Text size="supporting" color="muted">
        {label}
      </Text>
      <Text size="supporting" weight="emphasis" mono tabular>
        {value}
      </Text>
    </Stack>
  );
}
