import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Button variants — all sizes, shapes, and intents live here.
 *
 * ADR SD-2: variants are defined with CVA so callers compose classes without
 * conditional string building.
 */
const buttonVariants = cva(
  // Base: layout, typography, focus ring, transition
  // Note: `rounded-*` is NOT in the base — it lives in the `shape` variant so
  // callers can independently change border-radius without fighting the base.
  [
    'inline-flex items-center justify-center gap-2',
    'font-medium text-sm leading-none',
    'whitespace-nowrap select-none',
    'transition-colors duration-150',
    'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
    'disabled:pointer-events-none disabled:opacity-40',
    '[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0',
  ],
  {
    variants: {
      variant: {
        /** Primary CTA — accent fill */
        default: 'bg-accent text-accent-foreground hover:bg-accent-hover',

        /** Secondary — surface fill, visible border */
        secondary:
          'bg-surface-elevated text-foreground border border-border hover:bg-surface-overlay',

        /** Outline — transparent with border */
        outline: 'bg-transparent text-foreground border border-border hover:bg-surface-elevated',

        /** Ghost — no border, subtle hover */
        ghost: 'bg-transparent text-foreground hover:bg-surface-elevated',

        /**
         * Muted — like ghost but starts at a dimmed text colour and brightens
         * on hover. Use for secondary icon-label actions (e.g. Clear, Dismiss)
         * that should recede visually until the user focuses them.
         */
        muted: 'bg-transparent text-subtle hover:text-foreground hover:bg-surface-elevated',

        /** Destructive — error intent */
        destructive: 'bg-status-error text-foreground hover:bg-status-error/80',
      },

      size: {
        /**
         * Extra-small — compact pill actions (e.g. chat send button, tag chips).
         * Pairs with `shape="pill"` for fully rounded compact buttons.
         */
        xs: 'h-7 px-3 gap-1.5 text-xs',
        /** Small — secondary actions, dense toolbars. */
        sm: 'h-8 px-3 text-xs',
        /** Medium (default) — primary actions, form submits. */
        md: 'h-9 px-4',
        /** Large — prominent hero or modal CTAs. */
        lg: 'h-10 px-6 text-base',
        /**
         * Icon (square, medium) — icon-only buttons at standard touch target.
         * Always pair with an `aria-label`.
         */
        icon: 'h-9 w-9 p-0',
        /**
         * Icon-sm (square, small) — icon-only buttons in tight contexts such as
         * code block toolbars, inline actions, and dense list rows.
         * Always pair with an `aria-label`.
         */
        'icon-sm': 'h-7 w-7 p-0',
      },

      /**
       * Shape — controls the border-radius independently of size and variant.
       * Keeping shape separate lets callers combine any size with any radius
       * without needing per-combination overrides.
       *
       *   default — rounded-md, the standard Terra radius
       *   pill    — rounded-full, for compact action chips and send buttons
       */
      shape: {
        /** Standard Terra radius — use for the vast majority of buttons. */
        default: 'rounded-md',
        /**
         * Pill — fully rounded. Use for compact chat-style send buttons, tag
         * chips, and model-selector toggles.
         *
         * ```tsx
         * <Button size="xs" shape="pill"><Send />Send</Button>
         * ```
         */
        pill: 'rounded-full',
      },
    },

    defaultVariants: {
      variant: 'default',
      size: 'md',
      shape: 'default',
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  /**
   * When true, renders the button's children inside a `Slot` so the button
   * styles are applied to the child element (e.g. a Next.js `<Link>`).
   */
  asChild?: boolean;

  /**
   * When true, the button stretches to fill its container and aligns its
   * content to the left — the standard layout for sidebar navigation buttons
   * and full-width form actions.
   *
   * Equivalent to adding `w-full justify-start` without reaching for `className`.
   *
   * ```tsx
   * <Button variant="outline" fullWidth>
   *   <PenSquare /> New chat
   * </Button>
   * ```
   */
  fullWidth?: boolean;
}

/**
 * Button
 *
 * The primary interactive element of the Terra design system. Supports six
 * visual variants (`default`, `secondary`, `outline`, `ghost`, `muted`,
 * `destructive`), six sizes (`xs`, `sm`, `md`, `lg`, `icon`, `icon-sm`),
 * and two shapes (`default`, `pill`).
 *
 * Use `asChild` to apply button styles to an arbitrary child element such as
 * a router link. Use `fullWidth` to stretch the button across its container
 * with left-aligned content (e.g. sidebar navigation buttons).
 *
 * ```tsx
 * <Button>Deploy agent</Button>
 * <Button variant="secondary" size="sm">Cancel</Button>
 * <Button variant="muted" size="sm"><RotateCcw />Clear</Button>
 * <Button size="xs" shape="pill"><Send />Send</Button>
 * <Button variant="outline" fullWidth><PenSquare />New chat</Button>
 * <Button size="icon" variant="ghost" aria-label="Copy"><Copy /></Button>
 * <Button size="icon-sm" variant="ghost" aria-label="Copy"><Copy /></Button>
 * <Button asChild><Link href="/agents">View agents</Link></Button>
 * ```
 */
const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, shape, fullWidth = false, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp
        ref={ref}
        className={cn(
          buttonVariants({ variant, size, shape }),
          fullWidth && 'w-full justify-start',
          className,
        )}
        {...props}
      />
    );
  },
);

Button.displayName = 'Button';

export { Button, buttonVariants };
