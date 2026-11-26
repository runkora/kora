'use client';

/** @tier 1 — Zero kora domain knowledge. A list of options to search and pick from with the keyboard, on cmdk. */

import * as React from 'react';
import { Command } from 'cmdk';
import { cn } from '../../utils';

/** Keeps an item whose value or keywords contain the search, ignoring case, in the list's own order. */
function containsSearch(value: string, search: string, keywords?: string[]): number {
  const q = search.trim().toLowerCase();
  return [value, ...(keywords ?? [])].some((k) => k.toLowerCase().includes(q)) ? 1 : 0;
}

export type ComboboxProps = Omit<React.ComponentPropsWithoutRef<typeof Command>, 'className'>;

/**
 * The list's root. Arrow keys move the highlight and Enter picks it while focus is on the input or
 * the list. `label` names the input. `filter` replaces the default substring match.
 */
const Combobox = React.forwardRef<React.ComponentRef<typeof Command>, ComboboxProps>(
  ({ filter = containsSearch, ...props }, ref) => {
    const root = React.useRef<HTMLDivElement | null>(null);
    const setRoot = (el: HTMLDivElement | null) => {
      root.current = el;
      if (typeof ref === 'function') ref(el);
      else if (ref) ref.current = el;
    };
    // cmdk always renders a label for its input. Without an input, it would point at nothing.
    React.useLayoutEffect(() => {
      const label = root.current?.querySelector<HTMLLabelElement>(':scope > [cmdk-label]');
      if (!label) return;
      const input = root.current?.querySelector('[cmdk-input]');
      label.hidden = !input;
      if (input) label.htmlFor = input.id;
      else label.removeAttribute('for');
    });
    return (
      <Command
        ref={setRoot}
        data-slot="combobox"
        filter={filter}
        className="flex flex-col gap-1 outline-none"
        {...props}
      />
    );
  },
);
Combobox.displayName = 'Combobox';

export type ComboboxInputProps = Omit<
  React.ComponentPropsWithoutRef<typeof Command.Input>,
  'className'
>;

/** The search field, named by the `Combobox` label. Typing filters the items. */
const ComboboxInput = React.forwardRef<
  React.ComponentRef<typeof Command.Input>,
  ComboboxInputProps
>((props, ref) => (
  <Command.Input
    ref={ref}
    data-slot="combobox-input"
    className={cn(
      'flex h-8 w-full rounded-md border border-border bg-surface px-3 text-sm text-foreground',
      'placeholder:text-subtle',
      'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
    )}
    {...props}
  />
));
ComboboxInput.displayName = 'ComboboxInput';

export type ComboboxListProps = Omit<
  React.ComponentPropsWithoutRef<typeof Command.List>,
  'className'
>;

/**
 * The items, scrolling past 24rem. `label` names the listbox for screen readers. Without an input,
 * focus the list on open: it holds the highlighted item for assistive technology.
 */
const ComboboxList = React.forwardRef<React.ComponentRef<typeof Command.List>, ComboboxListProps>(
  (props, ref) => (
    <Command.List
      ref={ref}
      data-slot="combobox-list"
      className={cn(
        'max-h-96 overflow-y-auto overscroll-contain rounded-sm',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      )}
      {...props}
    />
  ),
);
ComboboxList.displayName = 'ComboboxList';

export type ComboboxItemProps = Omit<
  React.ComponentPropsWithoutRef<typeof Command.Item>,
  'className' | 'disabled'
> & { disabled?: boolean | undefined };

/**
 * One option. `value` must be unique in the list. `keywords` are matched by the search as well as
 * the value. A `disabled` item stays in the list and cannot be picked.
 */
const ComboboxItem = React.forwardRef<React.ComponentRef<typeof Command.Item>, ComboboxItemProps>(
  ({ disabled, ...props }, ref) => (
    <Command.Item
      ref={ref}
      data-slot="combobox-item"
      disabled={!!disabled}
      className={cn(
        'flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm text-foreground outline-none',
        'data-[selected=true]:bg-surface-overlay',
        'data-[disabled=true]:cursor-not-allowed data-[disabled=true]:opacity-50',
      )}
      {...props}
    />
  ),
);
ComboboxItem.displayName = 'ComboboxItem';

export type ComboboxEmptyProps = Omit<
  React.ComponentPropsWithoutRef<typeof Command.Empty>,
  'className'
>;

/** Shown only when the search matches no item. */
const ComboboxEmpty = React.forwardRef<
  React.ComponentRef<typeof Command.Empty>,
  ComboboxEmptyProps
>((props, ref) => (
  <Command.Empty
    ref={ref}
    data-slot="combobox-empty"
    className="px-2 py-1.5 text-xs text-subtle"
    {...props}
  />
));
ComboboxEmpty.displayName = 'ComboboxEmpty';

export { Combobox, ComboboxInput, ComboboxList, ComboboxItem, ComboboxEmpty };
