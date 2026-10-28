import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { InlineToken } from './inline-token';

describe('InlineToken', () => {
  it('is a button that does not submit a surrounding form', () => {
    render(<InlineToken>is</InlineToken>);
    expect(screen.getByRole('button', { name: 'is' })).toHaveAttribute('type', 'button');
  });

  it('shows a chevron, flipped while open, and drops it when asked', () => {
    const { container, rerender } = render(<InlineToken open={false}>is</InlineToken>);
    const closed = container.querySelector('svg')?.outerHTML;
    rerender(<InlineToken open>is</InlineToken>);
    expect(container.querySelector('svg')?.outerHTML).not.toBe(closed);
    rerender(<InlineToken chevron={false}>is</InlineToken>);
    expect(container.querySelector('svg')).toBeNull();
  });

  it('tints a value that failed validation', () => {
    render(<InlineToken invalid>is</InlineToken>);
    expect(screen.getByRole('button')).toHaveClass('bg-status-error/10');
  });

  it('can be disabled', () => {
    render(<InlineToken disabled>is</InlineToken>);
    expect(screen.getByRole('button')).toBeDisabled();
  });
});
