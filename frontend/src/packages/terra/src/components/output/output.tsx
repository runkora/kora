import * as React from 'react';
import { cn } from '../../utils';

export interface OutputProps {
  children: React.ReactNode;
  /** Maps to the HTML `for` attribute on `<output>`. */
  htmlFor?: string;
  /** When true, renders with input-like styling (height, border, bg, padding). */
  field?: boolean;
}

/**
 * Output
 *
 * A semantic `<output>` element for displaying computed or user-submitted
 * values. Associates with form controls via `htmlFor`.
 *
 * When `field` is true the output renders with input-like styling — useful
 * when a read-only computed value appears alongside editable inputs.
 *
 * ```tsx
 * // Plain output
 * <Output htmlFor="price-input">{total}</Output>
 *
 * // Input-like styled output
 * <Output field htmlFor="base-price">
 *   {formattedPrice}
 * </Output>
 * ```
 */
const Output = ({ children, htmlFor, field = false }: OutputProps) => {
  return (
    <output
      htmlFor={htmlFor}
      className={
        field
          ? cn(
              'inline-flex items-center h-9 rounded-md border border-border bg-surface px-3 text-sm truncate min-w-0',
            )
          : undefined
      }
    >
      {children}
    </output>
  );
};

Output.displayName = 'Output';

export { Output };
