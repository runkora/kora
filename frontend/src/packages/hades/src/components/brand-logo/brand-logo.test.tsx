import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { BrandLogo } from './brand-logo';

describe('BrandLogo', () => {
  it('renders both mode variants with the dark one shown and the light one hidden by default', () => {
    const { container } = render(<BrandLogo dark="/dark.svg" light="/light.svg" size={30} />);
    const dark = container.querySelector<HTMLImageElement>('img[data-mode="dark"]')!;
    const light = container.querySelector<HTMLImageElement>('img[data-mode="light"]')!;
    expect(dark.getAttribute('src')).toBe('/dark.svg');
    expect(light.getAttribute('src')).toBe('/light.svg');
    expect(dark.style.height).toBe('30px');
    expect(light.className).toContain('hidden');
    expect(dark.className).not.toMatch(/(^|\s)hidden(\s|$)/);
  });

  it('is an image named by title, and decorative without one', () => {
    const { container, rerender } = render(
      <BrandLogo dark="/d.svg" light="/l.svg" title="Example" />,
    );
    expect(screen.getByRole('img', { name: 'Example' })).toBeInTheDocument();
    for (const img of container.querySelectorAll('img')) expect(img.getAttribute('alt')).toBe('');

    rerender(<BrandLogo dark="/d.svg" light="/l.svg" />);
    expect(screen.queryByRole('img')).toBeNull();
    expect(document.querySelector('[data-slot="brand-logo"]')).toHaveAttribute(
      'aria-hidden',
      'true',
    );
  });
});
