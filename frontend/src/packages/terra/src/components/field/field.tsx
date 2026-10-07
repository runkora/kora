import * as React from 'react';
import { cn } from '../../utils';

/* ─── Field ──────────────────────────────────────────────────────────────── */

export type FieldProps = React.HTMLAttributes<HTMLDivElement>;

/**
 * Field
 *
 * A layout wrapper that groups a label, input, and helper/error text into a
 * cohesive form field. It imposes no opinion on the input element — place any
 * Terra input primitive (or native input) as a child.
 *
 * Compose with `Label`, `FieldHint`, and `FieldError` for a complete field:
 *
 * ```tsx
 * <Field>
 *   <Label htmlFor="email">Email</Label>
 *   <Input id="email" type="email" />
 *   <FieldHint>We'll never share your email.</FieldHint>
 * </Field>
 * ```
 *
 * With a validation error:
 *
 * ```tsx
 * <Field>
 *   <Label htmlFor="password">Password</Label>
 *   <Input id="password" type="password" hasError />
 *   <FieldError>Password must be at least 8 characters.</FieldError>
 * </Field>
 * ```
 */
const Field = React.forwardRef<HTMLDivElement, FieldProps>(({ className, ...props }, ref) => (
  <div ref={ref} className={cn('flex flex-col gap-2', className)} {...props} />
));
Field.displayName = 'Field';

/* ─── FieldHint ──────────────────────────────────────────────────────────── */

export type FieldHintProps = React.HTMLAttributes<HTMLParagraphElement>;

/**
 * FieldHint
 *
 * Supplementary helper text rendered below the input. Use for instructions or
 * constraints that the user needs before submitting.
 */
const FieldHint = React.forwardRef<HTMLParagraphElement, FieldHintProps>(
  ({ className, ...props }, ref) => (
    <p ref={ref} className={cn('text-xs text-muted', className)} {...props} />
  ),
);
FieldHint.displayName = 'FieldHint';

/* ─── FieldError ─────────────────────────────────────────────────────────── */

export interface FieldErrorProps extends React.HTMLAttributes<HTMLParagraphElement> {
  /** When false the element is hidden but still occupies layout space, preventing
   *  the form from jumping when an error appears. Defaults to true. */
  visible?: boolean;
}

/**
 * FieldError
 *
 * Validation error text rendered below the input. The `visible` prop controls
 * whether the message is shown — when false the element remains in the DOM
 * (preserving layout) but becomes invisible.
 */
const FieldError = React.forwardRef<HTMLParagraphElement, FieldErrorProps>(
  ({ className, visible = true, ...props }, ref) => (
    <p
      ref={ref}
      role="alert"
      aria-live="polite"
      className={cn('text-xs text-status-error', !visible && 'invisible', className)}
      {...props}
    />
  ),
);
FieldError.displayName = 'FieldError';

export { Field, FieldHint, FieldError };
