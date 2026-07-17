import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Input } from './input';

describe('Input', () => {
  it('renders a bare input with no wrapper when no adornments are set', () => {
    const { container } = render(<Input aria-label="Name" />);
    expect(container.firstElementChild?.tagName).toBe('INPUT');
    expect(container.querySelector('[data-slot="input-adorned"]')).toBeNull();
  });

  it('wraps the input and renders both adornments when set', () => {
    const { container } = render(
      <Input aria-label="Price" startAdornment="$" endAdornment="/ 1M tokens" />,
    );
    const wrapper = container.querySelector('[data-slot="input-adorned"]');
    expect(wrapper).not.toBeNull();
    expect(container.querySelector('[data-slot="input-start-adornment"]')).toHaveTextContent('$');
    expect(container.querySelector('[data-slot="input-end-adornment"]')).toHaveTextContent(
      '/ 1M tokens',
    );
  });

  it('insets the input text to clear the adornments', () => {
    render(<Input aria-label="Price" startAdornment="$" endAdornment="/ 1M tokens" />);
    const input = screen.getByLabelText('Price');
    expect(input).toHaveClass('pl-7', 'pr-16');
  });

  it('does not inset when only one adornment is set', () => {
    render(<Input aria-label="Price" startAdornment="$" />);
    const input = screen.getByLabelText('Price');
    expect(input).toHaveClass('pl-7');
    expect(input).not.toHaveClass('pr-16');
  });

  it('keeps value and onChange working with adornments', () => {
    const onChange = vi.fn();
    render(<Input aria-label="Price" startAdornment="$" defaultValue="1.00" onChange={onChange} />);
    const input = screen.getByLabelText<HTMLInputElement>('Price');
    fireEvent.change(input, { target: { value: '3.00' } });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(input.value).toBe('3.00');
  });

  it('marks adornments as non-interactive', () => {
    const { container } = render(<Input aria-label="Price" startAdornment="$" />);
    expect(container.querySelector('[data-slot="input-start-adornment"]')).toHaveClass(
      'pointer-events-none',
    );
  });
});
