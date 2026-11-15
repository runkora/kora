import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/** @tier 1 — Zero kora domain knowledge. A person's initials in a tinted circle. */

/** Sizes match Icon's container sizes, so an avatar and an icon circle line up in a column. */
const avatarVariants = cva(
  [
    'inline-flex shrink-0 items-center justify-center rounded-full',
    'bg-accent/15 text-accent font-semibold leading-none select-none uppercase',
  ],
  {
    variants: {
      size: {
        /** 20px, beside `Icon size="xs" container`. */
        xs: 'size-5 text-[9px]',
        /** 24px, beside `Icon size="2xs" container`. */
        '2xs': 'size-6 text-[10px]',
        /** 28px, beside `Icon size="sm" container`. */
        sm: 'size-7 text-xs',
        /** 36px, beside `Icon size="md" container`. */
        md: 'size-9 text-sm',
      },
    },
    defaultVariants: { size: '2xs' },
  },
);

/** Up to two initials from a name: "Dana Lee" is "DL", "dana.lee@example.com" is "DC". */
export function initialsOf(name: string): string {
  const parts = name.split(/[\s@._-]+/).filter(Boolean);
  const first = parts[0]?.[0] ?? '';
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? '') : '';
  return (first + last).toUpperCase();
}

export interface AvatarProps extends VariantProps<typeof avatarVariants> {
  /** The person's name; the avatar shows its initials. */
  name: string;
  /**
   * Accessible label. Without it the avatar is decorative and hidden from screen readers,
   * for when the name is already said nearby, such as in a tooltip trigger's label.
   */
  label?: string;
  /** @internal className is locked — all styling must go through typed props. */
  className?: never;
}

/**
 * Avatar
 *
 * A person's initials in a round, accent-tinted mark. It has no image: it marks who did
 * something in a dense row, next to icon circles of the same size.
 *
 * ```tsx
 * <Avatar name="Dana Lee" />
 * <Avatar name="Dana Lee" size="sm" label="Dana Lee" />
 * ```
 */
const Avatar = React.forwardRef<HTMLSpanElement, AvatarProps>(({ name, label, size }, ref) => (
  <span
    ref={ref}
    data-slot="avatar"
    role={label ? 'img' : undefined}
    aria-label={label}
    aria-hidden={label ? undefined : true}
    className={cn(avatarVariants({ size }))}
  >
    {initialsOf(name)}
  </span>
));
Avatar.displayName = 'Avatar';

export { Avatar, avatarVariants };
