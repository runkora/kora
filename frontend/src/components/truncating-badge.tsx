/**
 * @tier 2 — App-level shared. Used by connections; the tables that follow the
 * table-layout golden path use it for any badge whose label can grow.
 * @promote-to-tier-1 if: a second surface outside the dashboard needs it. It has
 * no Orbit domain knowledge, so by the placement decision tree it is a Terra
 * candidate; that needs human approval before any Terra change.
 */

import { Badge, Stack, Text } from '@orbit/terra';

/**
 * A badge whose label shrinks to the width its container allows, ending in an
 * ellipsis with the full label as the tooltip. The stretched, shrinkable
 * wrapper gives the badge a definite width; unconstrained it stays natural size.
 * Place it inside a row/wrap `Stack` (not directly in a block cell), otherwise
 * the wrapper stretches to the container's full width.
 */
export function TruncatingBadge({
  label,
  variant,
  textColor,
}: {
  label: string;
  variant: 'secondary' | 'outline';
  textColor: 'muted' | 'foreground';
}) {
  return (
    <Stack align="stretch" minWidth="none" overflow="hidden">
      <Badge variant={variant} size="sm" title={label}>
        <Text as="span" size="caption" color={textColor} truncate>
          {label}
        </Text>
      </Badge>
    </Stack>
  );
}
