import * as React from 'react';
import { Badge } from '../badge';
import { Stack } from '../layout/stack';
import { Text } from '../typography';

/** @tier 1 — Zero kora domain knowledge. A badge whose label shrinks to its container. */

export interface TruncatingBadgeProps {
  label: string;
  variant: 'secondary' | 'outline' | 'elevated';
  textColor: 'muted' | 'foreground';
}

/**
 * TruncatingBadge
 *
 * A badge whose label shrinks to the width its container allows, ending in an
 * ellipsis with the full label as the tooltip. The stretched, shrinkable
 * wrapper gives the badge a definite width; unconstrained it stays natural size.
 * Place it inside a row or wrap `Stack` (not directly in a block cell),
 * otherwise the wrapper stretches to the container's full width.
 *
 * ```tsx
 * <Stack direction="horizontal" minWidth="none">
 *   <TruncatingBadge label={name} variant="outline" textColor="foreground" />
 * </Stack>
 * ```
 */
const TruncatingBadge = ({ label, variant, textColor }: TruncatingBadgeProps) => (
  <Stack align="stretch" minWidth="none" overflow="hidden">
    <Badge variant={variant} size="sm" title={label}>
      <Text as="span" size="caption" color={textColor} truncate>
        {label}
      </Text>
    </Badge>
  </Stack>
);
TruncatingBadge.displayName = 'TruncatingBadge';

export { TruncatingBadge };
