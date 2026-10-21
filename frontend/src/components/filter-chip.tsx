/**
 * @tier 2 — App-level shared control (no kora domain knowledge). Used by: policy (grants,
 * vocabulary), mcp-gateway.
 *
 * A pill filter button with a trailing count, for a row of mutually exclusive filters above a
 * card view.
 */
import { Button, Text } from '@runkora/hades';

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
