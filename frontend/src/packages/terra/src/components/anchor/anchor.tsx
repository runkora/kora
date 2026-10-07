import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Anchor size variants — control the font size of the anchor text.
 *
 *   sm      — text-xs; use for secondary links in form layouts such as
 *             "Forgot password?" that sit alongside a field label.
 *   md      — text-sm; suits most inline body-copy contexts.
 *   inherit — no font-size class; inherits size from its parent.
 *             Use when the Anchor sits inside a Text or Heading component.
 */
const anchorVariants = cva(
  'text-accent underline underline-offset-4 hover:no-underline transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent rounded-sm',
  {
    variants: {
      size: {
        /** Compact — text-xs. For secondary form links like "Forgot password?". */
        sm: 'text-xs',
        /** Default — text-sm. Suits most inline body-copy contexts. */
        md: 'text-sm',
        /** Inherit — no font-size applied; inherits from parent element. */
        inherit: '',
      },
    },
    defaultVariants: {
      size: 'inherit',
    },
  },
);

export interface AnchorProps
  extends React.AnchorHTMLAttributes<HTMLAnchorElement>, VariantProps<typeof anchorVariants> {
  /**
   * When true, merges Anchor styles into the immediate child element instead
   * of rendering a native `<a>` tag. Used internally by app-level link
   * components (`AppLink`, `ExternalLink`) to avoid nesting `<a>` inside `<a>`.
   */
  asChild?: boolean;
}

/**
 * Anchor
 *
 * A pure styling primitive — applies Terra's accent colour, hover underline,
 * and focus ring to any anchor-like element. Carries no routing or navigation
 * logic of its own.
 *
 * In application code, prefer the app-level composites:
 *   - `AppLink`      — internal navigation (Next.js router + Anchor styling)
 *   - `ExternalLink` — external URLs (new tab, safe rel + Anchor styling)
 *
 * Use `Anchor` directly only inside the Terra design system itself, or in
 * exceptional cases where neither `AppLink` nor `ExternalLink` fits.
 *
 * Use `size="sm"` for secondary form links (e.g. "Forgot password?") that
 * must be smaller than surrounding body text.
 *
 * Inside body copy (external):
 * ```tsx
 * <ExternalLink url="https://docs.orbit.dev">Read the docs</ExternalLink>
 * ```
 *
 * Internal navigation:
 * ```tsx
 * <AppLink to="/">Go to Dashboard</AppLink>
 * ```
 *
 * Direct use (rare):
 * ```tsx
 * <Anchor asChild size="sm">
 *   <NextLink href="/forgot-password">Forgot password?</NextLink>
 * </Anchor>
 * ```
 */
const Anchor = React.forwardRef<HTMLAnchorElement, AnchorProps>(
  ({ className, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'a';
    return <Comp ref={ref} className={cn(anchorVariants({ size }), className)} {...props} />;
  },
);
Anchor.displayName = 'Anchor';

export { Anchor, anchorVariants };
