/**
 * @tier 3 — policy scoped.
 * @promote-to-tier-2 if: a second feature area outside policy adds filter tabs with count badges.
 */
import { Button, Text } from '@orbit/terra';

export interface FilterChipProps {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  /** Render the label in a monospace typeface — use for resource/type identifiers. */
  mono?: boolean;
}

export function FilterChip({ label, count, active, onClick, mono }: FilterChipProps) {
  return (
    <Button
      variant={active ? 'accent-outline' : 'outline-muted'}
      size="xs"
      shape="pill"
      onClick={onClick}
    >
      {mono ? (
        <Text as="span" size="micro" mono>
          {label}
        </Text>
      ) : (
        label
      )}
      <Text as="span" size="micro" color={active ? 'accent' : 'subtle'}>
        {count}
      </Text>
    </Button>
  );
}
