/**
 * @tier 2 — App-level shared. Cross-feature; used by policy (all tabs) and
 * model-gateway (keys tab). Renders an Orbit scope string with syntax
 * highlighting: resource type in accent, separators in subtle, action in foreground.
 * @promote-to-tier-1 if: the Orbit scope format (resource:path:action) is
 * removed and a generic token chip is needed by a second non-dashboard surface.
 */
import { Fragment } from 'react';
import { Badge, Text } from '@orbit/terra';

export interface ScopeChipProps {
  scope: string;
}

export function ScopeChip({ scope }: ScopeChipProps) {
  const parts = scope.split(':');
  return (
    <Badge variant="elevated" size="sm" mono>
      {parts.map((part, i) => {
        const isFirst = i === 0;
        const isLast = i === parts.length - 1;
        return (
          <Fragment key={i}>
            {i > 0 && (
              <Text as="span" size="caption" color="subtle" mono>
                :
              </Text>
            )}
            <Text
              as="span"
              size="caption"
              color={isFirst ? 'accent' : isLast ? 'foreground' : 'muted'}
              mono
            >
              {part}
            </Text>
          </Fragment>
        );
      })}
    </Badge>
  );
}
