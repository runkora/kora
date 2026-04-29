import * as React from 'react';
import { cn } from '../../utils';

export interface ButtonGroupProps {
  children: React.ReactNode;
  className?: string;
  id?: string;
  role?: React.AriaRole;
  'aria-label'?: string;
  'aria-labelledby'?: string;
}

/**
 * ButtonGroup
 *
 * Renders a horizontal row of buttons fused into a single segmented control.
 * Adjacent borders are collapsed (`-ml-px`) and inner corners of each child
 * button are squared off so only the outermost buttons retain their rounded
 * corners.
 *
 * Works with any Terra `<Button>` variant — including `asChild` buttons that
 * render as `<a>` or custom elements — because the selector targets all direct
 * children (`[&>*]`), not just `<button>` elements.
 *
 * ```tsx
 * <ButtonGroup>
 *   <Button variant={mode === 'cards' ? 'default' : 'outline'} size="icon-sm" ...>
 *     <Icon name="layout-grid" size="2xs" />
 *   </Button>
 *   <Button variant={mode === 'list' ? 'default' : 'outline'} size="icon-sm" ...>
 *     <Icon name="list" size="2xs" />
 *   </Button>
 * </ButtonGroup>
 * ```
 */
const ButtonGroup = React.forwardRef<HTMLDivElement, ButtonGroupProps>(
  ({ className, children, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={cn(
          'inline-flex',
          // Collapse adjacent borders into a single hairline.
          '[&>*:not(:first-child)]:-ml-px',
          // Square off right corners of every button except the last.
          '[&>*:not(:last-child)]:rounded-r-none',
          // Square off left corners of every button except the first.
          '[&>*:not(:first-child)]:rounded-l-none',
          className,
        )}
        {...props}
      >
        {children}
      </div>
    );
  },
);

ButtonGroup.displayName = 'ButtonGroup';

export { ButtonGroup };
