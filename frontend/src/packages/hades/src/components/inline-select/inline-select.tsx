'use client';

import * as React from 'react';
import { cn } from '../../utils';
import { Icon } from '../icon';
import { InlineToken } from '../inline-token';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';

/** @tier 1 — Zero kora domain knowledge. A short list of choices behind an inline word. */

export interface InlineSelectOption {
  value: string;
  label: string;
}

export interface InlineSelectProps {
  value: string;
  options: readonly InlineSelectOption[];
  onValueChange: (value: string) => void;
  /** Shown when `value` matches no option. */
  placeholder?: string | undefined;
  mono?: boolean | undefined;
  disabled?: boolean | undefined;
  'aria-label': string;
}

/**
 * InlineSelect
 *
 * A menu of a few choices, opened from an `InlineToken` so it reads as a word
 * in a sentence ("where payload.action **is** …"). For long or searchable
 * lists use a `Popover` with a catalog instead.
 *
 * ```tsx
 * <InlineSelect
 *   aria-label="Operator"
 *   value={op}
 *   onValueChange={setOp}
 *   options={[{ value: 'is', label: 'is' }, { value: 'contains', label: 'contains' }]}
 * />
 * ```
 */
function InlineSelect({
  value,
  options,
  onValueChange,
  placeholder = 'Choose',
  mono,
  disabled,
  'aria-label': ariaLabel,
}: InlineSelectProps) {
  const [open, setOpen] = React.useState(false);
  const selected = options.find((option) => option.value === value);
  const refs = React.useRef<(HTMLButtonElement | null)[]>([]);

  const move = (from: number, step: number) => {
    const next = (from + step + options.length) % options.length;
    refs.current[next]?.focus();
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <InlineToken
          mono={mono}
          open={open}
          disabled={disabled}
          aria-label={ariaLabel}
          tone={selected ? 'foreground' : 'subtle'}
        >
          {selected?.label ?? placeholder}
        </InlineToken>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-auto min-w-[8rem]"
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          const index = Math.max(
            options.findIndex((option) => option.value === value),
            0,
          );
          refs.current[index]?.focus();
        }}
      >
        <div role="listbox" aria-label={`${ariaLabel} options`} data-slot="inline-select-list">
          {options.map((option, index) => (
            <button
              key={option.value}
              ref={(node) => {
                refs.current[index] = node;
              }}
              type="button"
              role="option"
              aria-selected={option.value === value}
              data-slot="inline-select-option"
              className={cn(
                'flex w-full items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-left text-sm',
                'text-foreground hover:bg-surface-overlay focus-visible:bg-surface-overlay',
                'focus-visible:outline-none',
                mono && 'font-mono',
              )}
              onClick={() => {
                setOpen(false);
                onValueChange(option.value);
              }}
              onKeyDown={(event) => {
                if (event.key === 'ArrowDown') {
                  event.preventDefault();
                  move(index, 1);
                } else if (event.key === 'ArrowUp') {
                  event.preventDefault();
                  move(index, -1);
                }
              }}
            >
              {option.label}
              {option.value === value && <Icon name="check" size="2xs" color="muted" />}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
InlineSelect.displayName = 'InlineSelect';

export { InlineSelect };
