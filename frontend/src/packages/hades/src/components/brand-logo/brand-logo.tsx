import * as React from 'react';
import { cn } from '../../utils';

/** @tier 1 — Zero kora domain knowledge. Renders whatever logo a theme supplies, one image per mode. */

export interface BrandLogoProps extends Omit<React.ComponentPropsWithoutRef<'span'>, 'children'> {
  /** Image source for dark mode. */
  dark: string;
  /** Image source for light mode. */
  light: string;
  /** Height in pixels. The width follows the image's own proportions. Defaults to 28. */
  size?: number;
  /** Accessible name. Without it the logo is hidden from assistive technology. */
  title?: string;
}

/** BrandLogo renders one image per mode and CSS on `data-theme` shows the right one, with no flash. */
const BrandLogo = React.forwardRef<HTMLSpanElement, BrandLogoProps>(
  ({ dark, light, size = 28, title, className, ...props }, ref) => {
    const hidden = title === undefined;
    return (
      <span
        ref={ref}
        data-slot="brand-logo"
        role={hidden ? undefined : 'img'}
        aria-label={hidden ? undefined : title}
        aria-hidden={hidden ? true : undefined}
        className={cn('inline-flex shrink-0', className)}
        {...props}
      >
        <img
          src={dark}
          alt=""
          data-mode="dark"
          className="block w-auto [[data-theme='light']_&]:hidden"
          style={{ height: size }}
        />
        <img
          src={light}
          alt=""
          data-mode="light"
          className="hidden w-auto [[data-theme='light']_&]:block"
          style={{ height: size }}
        />
      </span>
    );
  },
);
BrandLogo.displayName = 'BrandLogo';

export { BrandLogo };
