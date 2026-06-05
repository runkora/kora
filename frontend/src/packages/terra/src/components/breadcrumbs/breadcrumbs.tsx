import * as React from 'react';
import { Icon } from '../icon/icon';

export interface BreadcrumbsItem {
  /** Text label for this segment. */
  label: string;
  /** Optional URL; renders an anchor when present. */
  href?: string;
}

export interface BreadcrumbsProps extends Omit<
  React.ComponentPropsWithoutRef<'nav'>,
  'children' | 'className'
> {
  /** Ordered segments, root first. The last item is treated as the current page. */
  items: BreadcrumbsItem[];
}

const Breadcrumbs = React.forwardRef<HTMLElement, BreadcrumbsProps>(
  ({ items, 'aria-label': ariaLabel = 'Breadcrumb', ...props }, ref) => (
    <nav ref={ref} aria-label={ariaLabel} data-slot="breadcrumbs" {...props}>
      <ol className="flex items-center gap-2">
        {items.map((item, index) => {
          const isCurrent = index === items.length - 1;
          return (
            <React.Fragment key={index}>
              {index > 0 && (
                <li aria-hidden="true" className="flex items-center">
                  <Icon name="chevron-right" size="2xs" color="muted" shrink />
                </li>
              )}
              <li className="flex items-center">
                {isCurrent ? (
                  <span aria-current="page" className="text-sm font-medium text-foreground">
                    {item.label}
                  </span>
                ) : item.href ? (
                  <a
                    href={item.href}
                    className="text-sm text-muted transition-colors duration-150 hover:text-foreground"
                  >
                    {item.label}
                  </a>
                ) : (
                  <span className="text-sm text-muted">{item.label}</span>
                )}
              </li>
            </React.Fragment>
          );
        })}
      </ol>
    </nav>
  ),
);

Breadcrumbs.displayName = 'Breadcrumbs';

export { Breadcrumbs };
