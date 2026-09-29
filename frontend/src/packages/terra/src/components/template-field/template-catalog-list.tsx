'use client';

import * as React from 'react';
import { cn } from '../../utils';
import { Input } from '../input';
import { SectionLabel } from '../section-label';
import { Text } from '../typography';
import { filterCatalog, groupCatalog, type TemplateCatalogEntry } from './template-model';

/** @tier 1 — Zero Orbit domain knowledge. The searchable, grouped field picker for TemplateField. */

export interface TemplateCatalogListProps {
  /** Every field the template may reference. */
  catalog: readonly TemplateCatalogEntry[];
  /**
   * One line naming what the catalog is scoped to, e.g. "Fields for push
   * deliveries". Shown above the results so a narrowed catalog visibly says
   * why it narrowed.
   */
  catalogLabel?: string;
  /** Called with the bare token, and its entry, when a row is chosen. */
  onSelect: (token: string, entry: TemplateCatalogEntry) => void;
  /** Focus the search box on mount. Defaults to true. */
  autoFocusSearch?: boolean;
  /** The search box placeholder, also its accessible name without the ellipsis. */
  searchPlaceholder?: string;
  /**
   * Let `pattern` entries be chosen. The caller completes the family into a
   * real token, e.g. by asking for the `<name>`.
   */
  selectablePatterns?: boolean;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
}

/**
 * The two-span body every catalog row shares: the mono token beside its
 * truncating sans description. Rendered identically by the pattern row and the
 * selectable row; only the token's colour differs.
 *
 * 11.5px is off the type scale on purpose: mono renders optically larger than
 * sans at the same size, so the token is nudged down to sit level with the
 * 11px sans description beside it. Do not round it to 11 or 12.
 */
const EntryBody = ({
  entry,
  tokenClassName,
}: {
  entry: TemplateCatalogEntry;
  tokenClassName: string;
}) => (
  <>
    <span className={cn('min-w-0 break-all font-mono text-[11.5px]', tokenClassName)}>
      {entry.token}
    </span>
    <span className="min-w-0 flex-1 truncate text-[11px] text-subtle" title={entry.description}>
      {entry.description}
    </span>
  </>
);

/**
 * TemplateCatalogList
 *
 * The searchable, grouped field picker inside TemplateField's popover. Split
 * out so it can be driven and tested without a floating layer: it is a plain
 * list with its own keyboard model, and knows nothing about where it is shown.
 *
 * Arrow keys move the active row across group boundaries, Enter inserts it.
 * `pattern` entries are shown but not selectable unless `selectablePatterns`
 * is set: the literal token describes a family, not a field that resolves.
 */
export const TemplateCatalogList = React.forwardRef<HTMLDivElement, TemplateCatalogListProps>(
  (
    {
      catalog,
      catalogLabel,
      onSelect,
      autoFocusSearch = true,
      searchPlaceholder = 'Search fields…',
      selectablePatterns = false,
      className,
    },
    ref,
  ) => {
    const [query, setQuery] = React.useState('');
    const [active, setActive] = React.useState(0);

    const filtered = React.useMemo(() => filterCatalog(catalog, query), [catalog, query]);
    const groups = React.useMemo(() => groupCatalog(filtered), [filtered]);
    const selectable = React.useMemo(
      () => (selectablePatterns ? filtered : filtered.filter((e) => !e.pattern)),
      [filtered, selectablePatterns],
    );

    // Keep the active row inside the (possibly shrunken) result set.
    React.useEffect(() => {
      setActive((current) => (current >= selectable.length ? 0 : current));
    }, [selectable.length]);

    const indexOf = React.useCallback(
      (entry: TemplateCatalogEntry) => selectable.indexOf(entry),
      [selectable],
    );

    const move = React.useCallback(
      (delta: number) => {
        if (selectable.length === 0) return;
        setActive((current) => {
          const next = current + delta;
          if (next < 0) return selectable.length - 1;
          if (next >= selectable.length) return 0;
          return next;
        });
      },
      [selectable.length],
    );

    const handleKeyDown = React.useCallback(
      (event: React.KeyboardEvent) => {
        if (event.key === 'ArrowDown') {
          event.preventDefault();
          move(1);
        } else if (event.key === 'ArrowUp') {
          event.preventDefault();
          move(-1);
        } else if (event.key === 'Enter') {
          event.preventDefault();
          const entry = selectable[active];
          if (entry) onSelect(entry.token, entry);
        }
      },
      [active, move, onSelect, selectable],
    );

    return (
      <div
        ref={ref}
        data-slot="template-catalog"
        className={cn('flex w-full min-w-0 flex-col gap-1', className)}
        onKeyDown={handleKeyDown}
      >
        <Input
          compact
          autoFocus={autoFocusSearch}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={searchPlaceholder}
          aria-label={searchPlaceholder.replace(/…$/, '')}
          data-slot="template-catalog-search"
        />

        {catalogLabel && (
          <Text size="micro" color="subtle" className="px-2 pt-1">
            {catalogLabel}
          </Text>
        )}

        <div
          className="max-h-64 overflow-y-auto overflow-x-hidden"
          data-slot="template-catalog-results"
        >
          {groups.length === 0 ? (
            <Text size="supporting" color="subtle" className="px-2 py-3">
              Nothing matches “{query}”.
            </Text>
          ) : (
            groups.map((group) => (
              <div key={group.name} className="pt-2 first:pt-1">
                <SectionLabel className="px-2 pb-1">{group.name}</SectionLabel>
                {group.entries.map((entry) => {
                  const index = indexOf(entry);
                  const isActive = index >= 0 && index === active;
                  return entry.pattern && !selectablePatterns ? (
                    <div
                      key={entry.token}
                      data-slot="template-catalog-pattern"
                      className="flex items-baseline gap-2 rounded-sm px-2 py-1 opacity-55"
                      aria-disabled="true"
                    >
                      <EntryBody entry={entry} tokenClassName="text-muted" />
                    </div>
                  ) : (
                    <button
                      key={entry.token}
                      type="button"
                      data-slot="template-catalog-item"
                      data-token={entry.token}
                      data-active={isActive ? 'true' : undefined}
                      onMouseEnter={() => setActive(index)}
                      onClick={() => onSelect(entry.token, entry)}
                      className={cn(
                        'flex w-full items-baseline gap-2 rounded-sm px-2 py-1 text-left transition-colors',
                        'hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent',
                        isActive && 'bg-surface-overlay',
                      )}
                    >
                      <EntryBody entry={entry} tokenClassName="text-foreground" />
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>
      </div>
    );
  },
);

TemplateCatalogList.displayName = 'TemplateCatalogList';
