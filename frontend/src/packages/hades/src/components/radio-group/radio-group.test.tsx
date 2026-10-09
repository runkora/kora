import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { RadioGroup, RadioGroupItem } from './radio-group';

describe('RadioGroup', () => {
  it('renders a radiogroup with radio items', () => {
    render(
      <RadioGroup defaultValue="a" aria-label="Options">
        <RadioGroupItem value="a" aria-label="Option A" />
        <RadioGroupItem value="b" aria-label="Option B" />
      </RadioGroup>,
    );
    expect(screen.getByRole('radiogroup', { name: 'Options' })).toBeInTheDocument();
    expect(screen.getAllByRole('radio')).toHaveLength(2);
  });

  it('marks the defaultValue item as checked', () => {
    render(
      <RadioGroup defaultValue="a" aria-label="Options">
        <RadioGroupItem value="a" aria-label="Option A" />
        <RadioGroupItem value="b" aria-label="Option B" />
      </RadioGroup>,
    );
    expect(screen.getByRole('radio', { name: 'Option A' })).toHaveAttribute(
      'data-state',
      'checked',
    );
    expect(screen.getByRole('radio', { name: 'Option B' })).toHaveAttribute(
      'data-state',
      'unchecked',
    );
  });

  it('selects an item on click and reports the value', () => {
    let selected: string | undefined;
    render(
      <RadioGroup
        defaultValue="a"
        onValueChange={(value) => {
          selected = value;
        }}
        aria-label="Options"
      >
        <RadioGroupItem value="a" aria-label="Option A" />
        <RadioGroupItem value="b" aria-label="Option B" />
      </RadioGroup>,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Option B' }));
    expect(selected).toBe('b');
    expect(screen.getByRole('radio', { name: 'Option B' })).toHaveAttribute(
      'data-state',
      'checked',
    );
  });

  it('renders the accent dot indicator only when checked', () => {
    const { container } = render(
      <RadioGroup defaultValue="a" aria-label="Options">
        <RadioGroupItem value="a" aria-label="Option A" />
        <RadioGroupItem value="b" aria-label="Option B" />
      </RadioGroup>,
    );
    expect(container.querySelectorAll('[data-slot="radio-group-indicator"]')).toHaveLength(1);
  });

  it('respects the disabled state', () => {
    render(
      <RadioGroup defaultValue="a" aria-label="Options">
        <RadioGroupItem value="a" aria-label="Option A" />
        <RadioGroupItem value="b" aria-label="Option B" disabled />
      </RadioGroup>,
    );
    expect(screen.getByRole('radio', { name: 'Option B' })).toBeDisabled();
  });

  it('applies data-slot attributes', () => {
    render(
      <RadioGroup defaultValue="a" aria-label="Options">
        <RadioGroupItem value="a" aria-label="Option A" />
      </RadioGroup>,
    );
    expect(screen.getByRole('radiogroup')).toHaveAttribute('data-slot', 'radio-group');
    expect(screen.getByRole('radio')).toHaveAttribute('data-slot', 'radio-group-item');
  });
});
