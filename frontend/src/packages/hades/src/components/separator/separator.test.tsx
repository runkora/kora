import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { Separator } from './separator';

describe('Separator', () => {
  it('is subtle by default', () => {
    const { container } = render(<Separator />);
    expect(container.firstElementChild?.className).toContain('bg-border-subtle');
  });

  it('uses the standard border colour when strong', () => {
    const { container } = render(<Separator tone="strong" />);
    const className = container.firstElementChild?.className ?? '';
    expect(className).toContain('bg-border');
    expect(className).not.toContain('bg-border-subtle');
  });
});
