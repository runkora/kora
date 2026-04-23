/**
 * Syntax-highlighted scope chip — renders `resource:action` with the resource
 * type in accent, separators in border color, and action in foreground.
 */
import { Fragment } from 'react';
import { Badge } from '@orbit/terra';

export function ScopeChip({ scope }: { scope: string }) {
  const parts = scope.split(':');
  return (
    <Badge variant="elevated" size="sm" mono>
      {parts.map((part, i) => {
        const isFirst = i === 0;
        const isLast = i === parts.length - 1;
        return (
          <Fragment key={i}>
            {i > 0 && <code className="text-border mx-px">:</code>}
            <code className={isFirst ? 'text-accent' : isLast ? 'text-foreground' : 'text-muted'}>
              {part}
            </code>
          </Fragment>
        );
      })}
    </Badge>
  );
}
