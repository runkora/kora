import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';
import { useFormField } from '../form-field';

// ─── Variants ────────────────────────────────────────────────────────────────

/**
 * Textarea visual variants.
 *
 *   default — surface background, rounded-md, px-3 py-2, visible border,
 *             standard focus ring. Use for standalone form fields.
 *
 *   bare    — transparent background, no border, no focus ring.
 *             Padding and border-radius are tuned for embedding inside a
 *             styled container (e.g. a chat input box that provides its own
 *             border and focus-within ring). The container handles the visual
 *             chrome; the Textarea provides only the interaction surface.
 *
 * Note: `padding` and `border-radius` are NOT in the shared base — they live
 * in each variant so that `bare` and `default` can independently set the
 * geometry that makes sense for their respective contexts.
 */
const textareaVariants = cva(
  [
    // Shared layout & typography
    'flex w-full',
    'text-sm text-foreground font-normal leading-[1.5]',
    'placeholder:text-subtle',
    'resize-none',
    'disabled:cursor-not-allowed disabled:opacity-40',
  ],
  {
    variants: {
      variant: {
        /**
         * Default — standalone form field with rounded-md, standard padding,
         * surface background, border, and a coloured focus ring.
         */
        default: [
          'rounded-md px-3 py-2',
          'bg-surface-elevated border border-border',
          'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        ],

        /**
         * Bare — for embedding inside a container that already provides its
         * own border and focus ring (e.g. the chat input box). Removes the
         * background, border, and focus outline so the container's styles are
         * the only visible chrome. The padding is tuned to a generous leading
         * and a tight bottom so the toolbar row below the input feels unified.
         *
         * ```tsx
         * <div className="rounded-2xl border focus-within:border-accent">
         *   <Textarea variant="bare" autoResize maxRows={6} />
         * </div>
         * ```
         */
        bare: [
          'rounded-2xl px-4 pt-3 pb-2',
          'bg-transparent border-0',
          'focus-visible:outline-none focus-visible:ring-0',
        ],
      },

      /**
       * Error state — overrides the border and focus ring colour with the
       * error status token. Only applied in the `default` variant; `bare`
       * delegates error visibility to its container.
       */
      error: {
        true: '',
        false: '',
      },
    },
    compoundVariants: [
      {
        variant: 'default',
        error: false,
        className: 'border-border focus-visible:outline-accent',
      },
      {
        variant: 'default',
        error: true,
        className: 'border-status-error focus-visible:outline-status-error',
      },
    ],
    defaultVariants: {
      variant: 'default',
      error: false,
    },
  },
);

// ─── Props ────────────────────────────────────────────────────────────────────

export interface TextareaProps
  extends
    React.TextareaHTMLAttributes<HTMLTextAreaElement>,
    Omit<VariantProps<typeof textareaVariants>, 'error'> {
  /**
   * Renders the textarea in an error state with a coloured border and
   * outline. Only visually meaningful in `variant="default"` — in `bare`
   * mode the container should communicate the error.
   */
  hasError?: boolean;

  /**
   * When true the textarea grows vertically as the user types, up to
   * `maxRows`. The height is controlled via an inline style — do NOT
   * set a fixed `rows` attribute alongside this prop.
   *
   * Resize is implemented on the synthetic `onChange` event so it fires
   * on every keystroke, paste, and autocomplete.
   */
  autoResize?: boolean;

  /**
   * Maximum number of visible rows before the textarea starts scrolling
   * internally. Only honoured when `autoResize` is true.
   *
   * @default 8
   */
  maxRows?: number;
}

// ─── Component ────────────────────────────────────────────────────────────────

/**
 * Textarea
 *
 * A multi-line text input styled to the Terra token system. Mirrors the
 * visual language of `Input` and adds an `autoResize` mode for chat and
 * note-taking interfaces.
 *
 * Use `variant="bare"` when embedding inside a container that already
 * provides its own border and focus ring — the outer element owns the chrome
 * and the Textarea owns only the interaction surface.
 *
 * Standalone form field:
 *
 * ```tsx
 * <Textarea rows={4} placeholder="Describe the task…" />
 * ```
 *
 * Auto-growing field with error state:
 *
 * ```tsx
 * <Textarea autoResize maxRows={6} hasError />
 * ```
 *
 * Embedded chat input — container provides the border and focus ring:
 *
 * ```tsx
 * <div className="rounded-2xl border border-border focus-within:border-accent">
 *   <Textarea
 *     variant="bare"
 *     autoResize
 *     maxRows={6}
 *     placeholder="Message Orbit…"
 *   />
 * </div>
 * ```
 */
const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  (
    {
      className,
      variant = 'default',
      hasError = false,
      autoResize = false,
      maxRows = 8,
      style,
      onChange,
      id,
      'aria-describedby': ariaDescribedby,
      'aria-invalid': ariaInvalid,
      'aria-required': ariaRequired,
      ...props
    },
    ref,
  ) => {
    const field = useFormField();
    // Line-height is 1.5 × 14px (text-sm) = 21px; add vertical padding
    // (pt-3=12px + pb-2=8px = 20px for bare; py-2=16px for default).
    const LINE_HEIGHT_PX = 21;
    const PADDING_PX = variant === 'bare' ? 20 : 16;
    const maxHeightPx = maxRows * LINE_HEIGHT_PX + PADDING_PX;

    const handleChange = React.useCallback(
      (e: React.ChangeEvent<HTMLTextAreaElement>) => {
        if (autoResize) {
          const el = e.currentTarget;
          // Reset then re-measure so shrinking also works.
          el.style.height = 'auto';
          el.style.height = `${Math.min(el.scrollHeight, maxHeightPx)}px`;
          el.style.overflowY = el.scrollHeight > maxHeightPx ? 'auto' : 'hidden';
        }
        onChange?.(e);
      },
      [autoResize, maxHeightPx, onChange],
    );

    return (
      <textarea
        ref={ref}
        onChange={handleChange}
        id={id ?? field?.id}
        aria-describedby={
          ariaDescribedby ??
          (field
            ? [field.hasHint ? field.hintId : '', field.hasError ? field.errorId : '']
                .filter(Boolean)
                .join(' ') || undefined
            : undefined)
        }
        aria-invalid={ariaInvalid ?? field?.hasError ?? undefined}
        aria-required={ariaRequired ?? field?.required ?? undefined}
        style={autoResize ? { overflowY: 'hidden', ...style } : style}
        className={cn(textareaVariants({ variant, error: hasError }), className)}
        {...props}
      />
    );
  },
);

Textarea.displayName = 'Textarea';

export { Textarea, textareaVariants };
