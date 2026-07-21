import * as React from 'react';
import { Field, FieldHint, FieldError } from '../field';
import { Label } from '../label';

// ── Context ───────────────────────────────────────────────────────────────────

export interface FormFieldContextValue {
  /** Stable id for the input element; use as `id` on the control. */
  id: string;
  /** Id for the hint element; use as part of `aria-describedby` on the control. */
  hintId: string;
  /** Id for the error element; use as part of `aria-describedby` on the control. */
  errorId: string;
  /** Whether a non-empty `error` prop is present on the parent `FormField`. */
  hasError: boolean;
  /** Whether a non-empty `hint` prop is present on the parent `FormField` (and no error). */
  hasHint: boolean;
  /** Whether the field is marked required. */
  required: boolean;
}

const FormFieldContext = React.createContext<FormFieldContextValue | null>(null);

/**
 * useFormField
 *
 * Reads the nearest `FormField` context. Terra input primitives (and custom
 * inputs) can call this inside their render function to automatically receive
 * the correct `id`, `aria-describedby`, and `aria-invalid` values without any
 * prop drilling.
 *
 * Returns `null` when the input is rendered outside a `FormField` — callers
 * should treat `null` as "no auto-wiring needed".
 *
 * ```tsx
 * const Input = React.forwardRef<HTMLInputElement, InputProps>((props, ref) => {
 *   const field = useFormField();
 *   return (
 *     <input
 *       ref={ref}
 *       id={field?.id}
 *       aria-describedby={field ? [field.hintId, field.errorId].join(' ') : undefined}
 *       aria-invalid={field?.hasError}
 *       aria-required={field?.required}
 *       {...props}
 *     />
 *   );
 * });
 * ```
 */
export function useFormField(): FormFieldContextValue | null {
  return React.useContext(FormFieldContext);
}

// ── FormField ─────────────────────────────────────────────────────────────────

export interface FormFieldProps {
  /**
   * Label text for the field. Rendered as an accessible `<label>` and
   * automatically associated with the control via `htmlFor`.
   */
  label: string;
  /**
   * Stable `id` for the control element. When omitted a React-generated id
   * is used — callers should still match this by consuming `useFormField()`.
   *
   * Pass explicitly when the id needs to be stable across SSR/CSR or when a
   * third-party input is used that cannot consume `useFormField()`.
   */
  id?: string;
  /** Supplementary hint rendered below the control. */
  hint?: string;
  /**
   * Validation error message. When non-empty the error text is shown and
   * `hasError: true` is placed in context so controls can apply error styling.
   */
  error?: string;
  /** Marks the field as required — visual asterisk and `aria-required` propagated. */
  required?: boolean;
  /**
   * Overrides the label font size.
   * - `'sm'` (default) — `text-sm`, standard form label size.
   * - `'xs'` — `text-xs`, compact label for dense cards or sidebars.
   */
  labelSize?: 'sm' | 'xs';
  children: React.ReactNode;
}

/**
 * FormField
 *
 * An opinionated wrapper that combines `Label`, an input slot, `FieldHint`,
 * and `FieldError` into a single component with proper accessibility wiring.
 *
 * The `id` → `htmlFor` connection, hint/error `aria-describedby`, and
 * `aria-invalid`/`aria-required` values are all managed automatically when
 * the inner control calls `useFormField()`.
 *
 * ```tsx
 * // Basic usage — Label and Field layout wired automatically
 * <FormField label="API token name" id="token-name">
 *   <Input id="token-name" placeholder="e.g. ci-read-only" />
 * </FormField>
 *
 * // With hint and validation error
 * <FormField
 *   label="Scopes"
 *   id="scopes"
 *   hint="Separate multiple scopes with a space."
 *   error={errors.scopes?.message}
 * >
 *   <Input id="scopes" hasError={!!errors.scopes} />
 * </FormField>
 *
 * // Required field
 * <FormField label="Principal" id="principal" required>
 *   <Input id="principal" aria-required="true" />
 * </FormField>
 * ```
 *
 * **Using auto-wiring**: Terra's built-in inputs (`Input`, `Textarea`, `NativeSelect`)
 * automatically call `useFormField()` and receive `{ id, hintId, errorId, hasError, required }`
 * to wire `aria-*` without any props. Custom controls can do the same.
 */
const FormField = ({
  label,
  id: idProp,
  hint,
  error,
  required = false,
  labelSize = 'sm',
  children,
}: FormFieldProps) => {
  const generatedId = React.useId();
  const id = idProp ?? generatedId;
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const hasError = Boolean(error);
  const hasHint = Boolean(hint) && !hasError;

  const contextValue: FormFieldContextValue = React.useMemo(
    () => ({ id, hintId, errorId, hasError, hasHint, required }),
    [id, hintId, errorId, hasError, hasHint, required],
  );

  return (
    <FormFieldContext.Provider value={contextValue}>
      <Field className={labelSize === 'xs' ? 'gap-1' : undefined}>
        <Label htmlFor={id} className={labelSize === 'xs' ? 'text-xs' : undefined}>
          {label}
          {required && (
            <span aria-hidden="true" className="ml-0.5 text-accent">
              *
            </span>
          )}
        </Label>
        {children}
        {hint && !hasError && <FieldHint id={hintId}>{hint}</FieldHint>}
        {hasError && <FieldError id={errorId}>{error}</FieldError>}
      </Field>
    </FormFieldContext.Provider>
  );
};
FormField.displayName = 'FormField';

export { FormField };
