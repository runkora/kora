import * as React from 'react';
import { cn } from '../../utils';
import { GAP, PADDING_BLOCK, PADDING_INLINE } from '../layout/spacing';
import type { SpacingSize } from '../layout/spacing';

export interface CheckboxProps {
  checked?: boolean;
  indeterminate?: boolean;
  onChange?: (checked: boolean, event: React.ChangeEvent<HTMLInputElement>) => void;
  children?: React.ReactNode;
  id?: string;
  name?: string;
  value?: string;
  disabled?: boolean;
  /** 'default' — inline checkbox+label. 'row' — full-width interactive row with hover + bottom border. */
  variant?: 'default' | 'row';
  gap?: SpacingSize;
  paddingBlock?: SpacingSize;
  paddingInline?: SpacingSize;
}

/**
 * Checkbox
 *
 * A styled checkbox input with an optional label. Supports an `indeterminate`
 * state and two layout variants:
 *
 * - `'default'` — inline checkbox + label (default).
 * - `'row'` — full-width interactive row with hover highlight and a bottom border.
 *
 * ```tsx
 * <Checkbox checked={selected} onChange={(checked) => setSelected(checked)}>
 *   Enable notifications
 * </Checkbox>
 *
 * <Checkbox variant="row" checked={allSelected} indeterminate={someSelected} onChange={toggleAll}>
 *   Select all
 * </Checkbox>
 * ```
 */
const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
  (
    {
      checked,
      indeterminate = false,
      onChange,
      children,
      id,
      name,
      value,
      disabled = false,
      variant = 'default',
      gap = 'snug',
      paddingBlock,
      paddingInline,
    },
    ref,
  ) => {
    const internalRef = React.useRef<HTMLInputElement>(null);
    const resolvedRef = (ref as React.RefObject<HTMLInputElement>) ?? internalRef;

    React.useEffect(() => {
      if (resolvedRef && 'current' in resolvedRef && resolvedRef.current) {
        resolvedRef.current.indeterminate = indeterminate;
      }
    }, [indeterminate, resolvedRef]);

    const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
      onChange?.(event.target.checked, event);
    };

    return (
      <label
        className={cn(
          'cursor-pointer select-none flex items-center',
          GAP.base[gap],
          paddingBlock && PADDING_BLOCK[paddingBlock],
          paddingInline && PADDING_INLINE[paddingInline],
          variant === 'row' &&
            'w-full border-b border-border-subtle hover:bg-surface-overlay transition-colors',
          disabled && 'pointer-events-none opacity-50',
        )}
      >
        <input
          ref={resolvedRef}
          type="checkbox"
          id={id}
          name={name}
          value={value}
          checked={checked}
          disabled={disabled}
          onChange={handleChange}
          className="accent-accent size-3.5 cursor-pointer rounded shrink-0"
        />
        {children}
      </label>
    );
  },
);

Checkbox.displayName = 'Checkbox';

export { Checkbox };
