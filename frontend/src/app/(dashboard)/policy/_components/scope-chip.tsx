/**
 * Syntax-highlighted scope chip — renders `resource:action` with the resource
 * type in accent, separators in border color, and action in foreground.
 */
import { Fragment } from 'react';

export function ScopeChip({ scope }: { scope: string }) {
  const parts = scope.split(':');
  return (
    // eslint-disable-next-line local/no-raw-div
    <div className="inline-flex items-center font-mono text-[0.6875rem] bg-surface-elevated border border-border-subtle rounded px-1.5 py-0.5 whitespace-nowrap">
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
    </div>
  );
}
