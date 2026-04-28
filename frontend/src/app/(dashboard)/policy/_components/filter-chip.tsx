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
}

export function FilterChip({ label, count, active, onClick }: FilterChipProps) {
  return (
    <Button variant={active ? 'secondary' : 'ghost'} size="xs" shape="pill" onClick={onClick}>
      {label}
      <Text as="span" size="micro" color="muted">
        {count}
      </Text>
    </Button>
  );
}
