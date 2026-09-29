import * as React from 'react';
import { cn } from '../../utils';
import { GAP, PADDING_BLOCK, PADDING_INLINE } from '../layout/spacing';
import type { SpacingSize } from '../layout/spacing';

/** Box dimensions per size, in px — tuned to the Terra spec. */
export type CheckboxSize = 'md' | 'lg';

const SIZES: Record<CheckboxSize, { box: number; radius: string; check: number }> = {
  md: { box: 20, radius: 'var(--radius-sm)', check: 12 },
  lg: { box: 22, radius: 'var(--radius-sm)', check: 13 },
};

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
  /** Box size. 'md' (20px, default) · 'lg' (22px). */
  size?: CheckboxSize;
  gap?: SpacingSize;
  paddingBlock?: SpacingSize;
  paddingInline?: SpacingSize;
  'aria-label'?: string;
}

/**
 * Checkbox
 *
 * A custom-drawn checkbox with an optional label. The visible box is fully
 * styled (rounded, accent-filled when checked, with a drawn check) so it renders
 * identically across browsers; a visually-hidden native `<input>` preserves
 * keyboard, form, and a11y semantics. Supports `indeterminate`, two `size`s,
 * and two layout variants:
 *
 * - `'default'` — inline checkbox + label (default).
 * - `'row'` — full-width interactive row with hover highlight and a bottom border.
 *
 * ```tsx
 * <Checkbox checked={selected} onChange={(checked) => setSelected(checked)}>
 *   Enable notifications
 * </Checkbox>
 *
 * <Checkbox size="lg" checked={on} onChange={toggle} aria-label="Select server" />
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
      size = 'md',
      gap = 'snug',
      paddingBlock,
      paddingInline,
      'aria-label': ariaLabel,
    },
    ref,
  ) => {
    const internalRef = React.useRef<HTMLInputElement>(null);
    React.useImperativeHandle(ref, () => internalRef.current!);

    React.useEffect(() => {
      if (internalRef.current) {
        internalRef.current.indeterminate = indeterminate;
      }
    }, [indeterminate]);

    const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
      onChange?.(event.target.checked, event);
    };

    const dim = SIZES[size];
    const active = !!checked || indeterminate;

    return (
      <label
        className={cn(
          'cursor-pointer select-none inline-flex items-center',
          GAP.base[gap],
          paddingBlock && PADDING_BLOCK[paddingBlock],
          paddingInline && PADDING_INLINE[paddingInline],
          variant === 'row' &&
            'w-full border-b border-border-subtle hover:bg-surface-overlay transition-colors',
          disabled && 'pointer-events-none opacity-50',
        )}
      >
        <input
          ref={internalRef}
          type="checkbox"
          id={id}
          name={name}
          value={value}
          checked={checked}
          disabled={disabled}
          aria-label={ariaLabel}
          onChange={handleChange}
          className="sr-only"
        />
        <span
          aria-hidden
          style={{
            width: dim.box,
            height: dim.box,
            flexShrink: 0,
            borderRadius: dim.radius,
            border: `1.5px solid ${active ? 'var(--color-accent)' : 'var(--color-border)'}`,
            background: active ? 'var(--color-accent)' : 'transparent',
            color: 'var(--color-accent-foreground)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            transition: 'all 150ms',
          }}
        >
          {indeterminate ? (
            <svg
              width={dim.check}
              height={dim.check}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={3}
              strokeLinecap="round"
            >
              <path d="M5 12h14" />
            </svg>
          ) : checked ? (
            <svg
              width={dim.check}
              height={dim.check}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={3}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M20 6 9 17l-5-5" />
            </svg>
          ) : null}
        </span>
        {children}
      </label>
    );
  },
);

Checkbox.displayName = 'Checkbox';

export { Checkbox };
