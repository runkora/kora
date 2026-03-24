import * as React from 'react';
import * as LabelPrimitive from '@radix-ui/react-label';
import { cn } from '../../utils';

/**
 * Label
 *
 * An accessible form label built on Radix UI's Label primitive. Associates
 * with any form control via the standard `htmlFor` prop and automatically
 * greys out when its paired control is disabled.
 *
 * ```tsx
 * <Label htmlFor="policy-name">Policy name</Label>
 * <Input id="policy-name" placeholder="e.g. read-only-s3" />
 * ```
 */
const Label = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(({ className, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={cn(
      'text-sm font-medium leading-none text-foreground',
      'peer-disabled:cursor-not-allowed peer-disabled:opacity-50',
      'select-none',
      className,
    )}
    {...props}
  />
));

Label.displayName = LabelPrimitive.Root.displayName;

export { Label };
