import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Eyebrow } from './eyebrow';

describe('Eyebrow', () => {
  it('renders its children in a span', () => {
    render(<Eyebrow>Audit</Eyebrow>);
    const el = screen.getByText('Audit');
    expect(el.tagName).toBe('SPAN');
  });

  it('applies the micro uppercase treatment', () => {
    render(<Eyebrow>Audit</Eyebrow>);
    const el = screen.getByText('Audit');
    expect(el).toHaveClass('text-[11px]', 'font-bold', 'uppercase', 'tracking-widest');
    expect(el).toHaveClass('text-subtle', 'leading-none');
  });

  it('carries the data-slot attribute', () => {
    render(<Eyebrow>Audit</Eyebrow>);
    expect(screen.getByText('Audit')).toHaveAttribute('data-slot', 'eyebrow');
  });
});
